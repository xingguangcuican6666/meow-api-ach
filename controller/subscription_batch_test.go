package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openSubscriptionBatchTestDBs starts the three databases this suite must run on
// (SQLite/MySQL/PostgreSQL) plus the audit log database, so the cross-user batch
// endpoints are exercised through real drivers rather than a single dialect.
func openSubscriptionBatchTestDBs(t *testing.T, dialect string) (*gorm.DB, *gorm.DB, common.DatabaseType) {
	t.Helper()
	var driver, logDriver gorm.Dialector
	dbType := common.DatabaseTypeSQLite
	switch dialect {
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
		logDSN := os.Getenv("TEST_MYSQL_LOG_DSN")
		if logDSN == "" {
			logDSN = dsn
		}
		logDriver = mysql.Open(logDSN)
		dbType = common.DatabaseTypeMySQL
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
		logDSN := os.Getenv("TEST_POSTGRES_LOG_DSN")
		if logDSN == "" {
			logDSN = dsn
		}
		logDriver = postgres.Open(logDSN)
		dbType = common.DatabaseTypePostgreSQL
	default:
		driver = sqlite.Open(":memory:")
		logDriver = sqlite.Open(":memory:")
	}

	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// A single connection is what makes an in-transaction clock read deadlock if
	// it goes through the shared handle, so keep the pool at one here.
	sqlDB.SetMaxOpenConns(1)

	logDB, err := gorm.Open(logDriver, &gorm.Config{})
	require.NoError(t, err)
	logSQL, err := logDB.DB()
	require.NoError(t, err)
	logSQL.SetMaxOpenConns(1)

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	model.DB, model.LOG_DB = db, logDB
	common.SetDatabaseTypes(dbType, dbType)
	common.RedisEnabled = false

	tables := []any{
		&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
	}
	// MySQL and PostgreSQL outlive a single run, so drop first instead of
	// asserting an empty database: a previous aborted run can leave tables
	// behind and would then make this suite fail before testing anything.
	require.NoError(t, db.Migrator().DropTable(tables...))
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, logDB.Migrator().DropTable(&model.AuditLog{}))
	require.NoError(t, logDB.AutoMigrate(&model.AuditLog{}))

	// One cleanup that drops, then closes: registering the closes separately
	// would tear down the connections the drops still need.
	t.Cleanup(func() {
		_ = logDB.Migrator().DropTable(&model.AuditLog{})
		_ = db.Migrator().DropTable(tables...)
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		_ = logSQL.Close()
		_ = sqlDB.Close()
	})

	return db, logDB, dbType
}

// TestSubscriptionBatchEndpoints covers the HTTP contract of the cross-user
// batch endpoints: input bounds, the payment-compliance gate on grant, partial
// success reporting, and what the audit trail records. The per-item model
// semantics are covered in model/subscription_batch_test.go; this file asserts
// only what an API caller and an auditor can observe.
func TestSubscriptionBatchEndpoints(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db, logDB, _ := openSubscriptionBatchTestDBs(t, dialect)
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			token := "subscription-batch-audit-token"
			admin := model.User{
				Username: "subscription-batch-admin", Password: "unused",
				Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
				Group: "default", AccessToken: &token,
			}
			require.NoError(t, db.Create(&admin).Error)

			plan := model.SubscriptionPlan{
				Title: "Batch plan", PriceAmount: 0,
				DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1,
				TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever,
				MaxPurchasePerUser: 1,
			}
			require.NoError(t, db.Create(&plan).Error)

			granted := model.User{
				Username: "batch-target", Password: "unused", AffCode: "batch-target-aff",
				Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
			}
			require.NoError(t, db.Create(&granted).Error)

			router := gin.New()
			router.Use(middleware.RequestId())
			group := router.Group("/api/subscription/admin")
			group.Use(middleware.AdminAuth())
			{
				group.GET("/subscriptions", AdminListSubscriptions)
				group.POST("/subscriptions/grant", AdminBatchGrantSubscription)
				group.POST("/subscriptions/invalidate", AdminBatchInvalidateSubscriptions)
				group.POST("/subscriptions/reset", AdminBatchResetSubscriptions)
				group.POST("/subscriptions/delete", AdminBatchDeleteSubscriptions)
			}

			post := func(path, body string) (*httptest.ResponseRecorder, map[string]any) {
				t.Helper()
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
				request.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(response, request)
				var decoded map[string]any
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
				return response, decoded
			}

			auditEvents := func(response *httptest.ResponseRecorder) []model.AuditLog {
				t.Helper()
				var events []model.AuditLog
				require.NoError(t, logDB.Where(
					"request_id = ? AND category = ?",
					response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation,
				).Find(&events).Error)
				return events
			}

			// ---- grant is gated on payment compliance, like the per-user path ----
			revokePaymentComplianceForTest(t)
			_, body := post("/api/subscription/admin/subscriptions/grant",
				fmt.Sprintf(`{"user_ids":[%d],"plan_id":%d}`, granted.Id, plan.Id))
			assert.Equal(t, false, body["success"], "grant must be refused before compliance is confirmed")
			var subsBefore []model.UserSubscription
			require.NoError(t, db.Find(&subsBefore).Error)
			assert.Empty(t, subsBefore, "a refused grant must not create a subscription")

			confirmPaymentComplianceForTest(t)

			// ---- empty and oversized batches are refused before any write ----
			oversizedIds := make([]string, 501)
			for index := range oversizedIds {
				oversizedIds[index] = fmt.Sprint(granted.Id)
			}
			oversized := fmt.Sprintf(`{"ids":[%s]}`, strings.Join(oversizedIds, ","))
			for _, route := range []string{
				"/api/subscription/admin/subscriptions/invalidate",
				"/api/subscription/admin/subscriptions/reset",
				"/api/subscription/admin/subscriptions/delete",
			} {
				for _, payload := range []string{`{"ids":[]}`, oversized} {
					response, decoded := post(route, payload)
					assert.Equal(t, false, decoded["success"], "%s with %.20s", route, payload)
					assert.NotEmpty(t, decoded["message"], "%s must say why it refused", route)
					events := auditEvents(response)
					require.Len(t, events, 1, "%s records one fallback audit entry", route)
					assert.False(t, events[0].Success)
				}
			}
			response, decoded := post("/api/subscription/admin/subscriptions/grant",
				fmt.Sprintf(`{"user_ids":[],"plan_id":%d}`, plan.Id))
			assert.Equal(t, false, decoded["success"])
			require.Len(t, auditEvents(response), 1)

			// ---- a batch grant that hits the purchase cap reports it per entry ----
			response, decoded = post("/api/subscription/admin/subscriptions/grant",
				fmt.Sprintf(`{"user_ids":[%d,999999],"plan_id":%d}`, granted.Id, plan.Id))
			require.Equal(t, true, decoded["success"])
			data := decoded["data"].(map[string]any)
			assert.EqualValues(t, 2, data["requested"])
			assert.EqualValues(t, 1, data["succeeded"])
			assert.EqualValues(t, 1, data["failed"])
			// Each entry must name its target so the operator can tell which user
			// failed: a grant entry is keyed by user id and plan id, not by a
			// subscription row id that does not exist yet.
			items, ok := data["items"].([]any)
			require.True(t, ok)
			require.Len(t, items, 2)
			first := items[0].(map[string]any)
			assert.EqualValues(t, granted.Id, first["user_id"])
			assert.EqualValues(t, plan.Id, first["plan_id"])
			assert.Equal(t, true, first["ok"])
			second := items[1].(map[string]any)
			assert.Equal(t, false, second["ok"])
			assert.NotEmpty(t, second["message"], "a failed entry says why")

			events := auditEvents(response)
			require.Len(t, events, 1, "one operation event, no duplicate fallback")
			event := events[0]
			assert.Equal(t, "subscription.batch_grant", event.Action)
			assert.True(t, event.Success)
			assert.Equal(t, admin.Id, event.UserId, "the audit entry belongs to the operator")
			require.NotNil(t, event.Other.Op)
			encoded, err := common.Marshal(event.Other.Op.Params)
			require.NoError(t, err)
			var params struct {
				Requested int   `json:"requested"`
				Succeeded int   `json:"succeeded"`
				Failed    int   `json:"failed"`
				PlanId    int   `json:"plan_id"`
				Ids       []int `json:"requested_subscription_ids"`
			}
			require.NoError(t, common.Unmarshal(encoded, &params))
			assert.Equal(t, 2, params.Requested)
			assert.Equal(t, 1, params.Succeeded)
			assert.Equal(t, 1, params.Failed)
			assert.Equal(t, plan.Id, params.PlanId)
			assert.Equal(t, []int{granted.Id, 999999}, params.Ids,
				"the audit trail names the users the batch targeted")
			encoded, err = common.Marshal(event)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), token, "an audit entry must not leak the operator token")

			// ---- the cross-user list resolves owner and plan names server-side ----
			listResponse := httptest.NewRecorder()
			listRequest := httptest.NewRequest(http.MethodGet,
				fmt.Sprintf("/api/subscription/admin/subscriptions?p=1&page_size=10&keyword=%s", granted.Username), nil)
			listRequest.Header.Set("Authorization", "Bearer "+token)
			router.ServeHTTP(listResponse, listRequest)
			var listed struct {
				Success bool `json:"success"`
				Data    struct {
					Total int64 `json:"total"`
					Items []struct {
						UserId   int    `json:"user_id"`
						Username string `json:"username"`
						PlanId   int    `json:"plan_id"`
						PlanName string `json:"plan_title"`
					} `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(listResponse.Body.Bytes(), &listed))
			require.True(t, listed.Success)
			assert.EqualValues(t, 1, listed.Data.Total)
			require.Len(t, listed.Data.Items, 1)
			assert.Equal(t, granted.Username, listed.Data.Items[0].Username)
			assert.Equal(t, "Batch plan", listed.Data.Items[0].PlanName)

			var subs []model.UserSubscription
			require.NoError(t, db.Find(&subs).Error)
			require.Len(t, subs, 1)

			// ---- invalidating a non-existent id fails that entry only ----
			response, decoded = post("/api/subscription/admin/subscriptions/invalidate",
				fmt.Sprintf(`{"ids":[%d,999999]}`, subs[0].Id))
			require.Equal(t, true, decoded["success"])
			data = decoded["data"].(map[string]any)
			assert.EqualValues(t, 1, data["succeeded"])
			assert.EqualValues(t, 1, data["failed"])
			require.Len(t, auditEvents(response), 1)

			var after model.UserSubscription
			require.NoError(t, db.Where("id = ?", subs[0].Id).First(&after).Error)
			assert.Equal(t, "cancelled", after.Status)

			// ---- delete removes the row and records its ids ----
			response, decoded = post("/api/subscription/admin/subscriptions/delete",
				fmt.Sprintf(`{"ids":[%d]}`, subs[0].Id))
			require.Equal(t, true, decoded["success"])
			events = auditEvents(response)
			require.Len(t, events, 1)
			assert.Equal(t, "subscription.batch_delete", events[0].Action)
			encoded, err = common.Marshal(events[0].Other.Op.Params)
			require.NoError(t, err)
			var deleteParams struct {
				Succeeded int   `json:"succeeded"`
				Ids       []int `json:"requested_subscription_ids"`
			}
			require.NoError(t, common.Unmarshal(encoded, &deleteParams))
			assert.Equal(t, 1, deleteParams.Succeeded)
			assert.Equal(t, []int{subs[0].Id}, deleteParams.Ids)

			var remaining int64
			require.NoError(t, db.Model(&model.UserSubscription{}).Count(&remaining).Error)
			assert.EqualValues(t, 0, remaining)
		})
	}
}

// A grant batch larger than the model-layer bound must be refused at the API
// boundary without opening a single transaction.
func TestSubscriptionBatchGrantOversizeRefused(t *testing.T) {
	db, logDB, _ := openSubscriptionBatchTestDBs(t, "sqlite")
	token := "subscription-batch-oversize-token"
	admin := model.User{
		Username: "subscription-batch-oversize-admin", Password: "unused",
		Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
		Group: "default", AccessToken: &token,
	}
	require.NoError(t, db.Create(&admin).Error)
	plan := model.SubscriptionPlan{
		Title: "Oversize plan", DurationUnit: model.SubscriptionDurationMonth,
		DurationValue: 1, TotalAmount: 100, QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, db.Create(&plan).Error)

	router := gin.New()
	router.Use(middleware.RequestId())
	group := router.Group("/api/subscription/admin")
	group.Use(middleware.AdminAuth())
	group.POST("/subscriptions/grant", AdminBatchGrantSubscription)

	confirmPaymentComplianceForTest(t)

	ids := make([]string, 501)
	for index := range ids {
		ids[index] = fmt.Sprint(index + 1)
	}
	payload := fmt.Sprintf(`{"user_ids":[%s],"plan_id":%d}`, strings.Join(ids, ","), plan.Id)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/subscription/admin/subscriptions/grant",
		bytes.NewBufferString(payload))
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(response, request)

	var decoded struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
	assert.False(t, decoded.Success)
	assert.NotEmpty(t, decoded.Message, "the refusal must name the bound")

	var subs int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Count(&subs).Error)
	assert.EqualValues(t, 0, subs, "an oversized grant must not create anything")

	// Filter by category: the middleware records the refusal as a generic
	// operation entry, while the access-token trail records the same request
	// separately. Both are correct; only the operation entry is this test's
	// subject.
	var events []model.AuditLog
	require.NoError(t, logDB.Where(
		"request_id = ? AND category = ?",
		response.Header().Get(common.RequestIdKey), model.AuditCategoryOperation,
	).Find(&events).Error)
	require.Len(t, events, 1)
	assert.False(t, events[0].Success)
}

// A non-admin must not reach any batch endpoint, and the failure must not write.
func TestSubscriptionBatchEndpointsRequireAdmin(t *testing.T) {
	db, logDB, _ := openSubscriptionBatchTestDBs(t, "sqlite")
	token := "subscription-batch-user-token"
	user := model.User{
		Username: "subscription-batch-user", Password: "unused",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AccessToken: &token,
	}
	require.NoError(t, db.Create(&user).Error)

	router := gin.New()
	router.Use(middleware.RequestId())
	group := router.Group("/api/subscription/admin")
	group.Use(middleware.AdminAuth())
	group.POST("/subscriptions/grant", AdminBatchGrantSubscription)
	group.POST("/subscriptions/invalidate", AdminBatchInvalidateSubscriptions)
	group.POST("/subscriptions/reset", AdminBatchResetSubscriptions)
	group.POST("/subscriptions/delete", AdminBatchDeleteSubscriptions)

	for _, route := range []string{
		"/api/subscription/admin/subscriptions/grant",
		"/api/subscription/admin/subscriptions/invalidate",
		"/api/subscription/admin/subscriptions/reset",
		"/api/subscription/admin/subscriptions/delete",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, route,
			bytes.NewBufferString(`{"ids":[1],"user_ids":[1],"plan_id":1}`))
		request.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(response, request)

		var decoded struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &decoded))
		assert.False(t, decoded.Success, "%s must reject an ordinary user", route)
	}

	var subs int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Count(&subs).Error)
	assert.EqualValues(t, 0, subs)

	// The access-token trail does record the attempt, which is intended; what a
	// rejected caller must not produce is an operation audit entry claiming a
	// batch action happened.
	var events []model.AuditLog
	require.NoError(t, logDB.Where("category = ?", model.AuditCategoryOperation).Find(&events).Error)
	assert.Empty(t, events, "a rejected caller leaves no operation audit entry")
}
