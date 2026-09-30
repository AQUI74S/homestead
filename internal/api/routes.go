package api

const (
	apiPrefix = "/api/"
	// callbackPath is where the bank sends the user back after the authorization.
	callbackPath = "/api/connections/callback"
)

type route struct {
	pattern string // method and path, see net/http.ServeMux
	handler handlerFunc
	public  bool // reachable without login
}

// routes lists all API endpoints. Everything except login and status needs a session.
func (s *Server) routes() []route {
	return []route{
		// Session
		{pattern: "GET /api/me", handler: s.me, public: true},
		{pattern: "POST /api/login", handler: s.login, public: true},
		{pattern: "POST /api/logout", handler: s.logout, public: true},

		// Household budget
		{pattern: "GET /api/overview", handler: s.overview},
		{pattern: "GET /api/categories", handler: s.categories},
		{pattern: "POST /api/categories", handler: s.createCategory},
		{pattern: "PUT /api/categories/{id}/budget", handler: s.setBudget},
		{pattern: "POST /api/budgets/suggest", handler: s.suggestBudgets},
		{pattern: "PUT /api/settings", handler: s.putSettings},

		// Transactions and rules
		{pattern: "GET /api/transactions", handler: s.transactions},
		{pattern: "PATCH /api/transactions/{id}", handler: s.patchTransaction},
		{pattern: "POST /api/accounts/{id}/import", handler: s.importCSV},
		{pattern: "GET /api/rules", handler: s.rules},
		{pattern: "DELETE /api/rules/{id}", handler: s.deleteRule},

		// Recurring payments and contracts
		{pattern: "GET /api/recurring", handler: s.recurring},
		{pattern: "POST /api/recurring", handler: s.createRecurring},
		{pattern: "PATCH /api/recurring/{id}", handler: s.patchRecurring},
		{pattern: "DELETE /api/recurring/{id}", handler: s.deleteRecurring},

		// Banks, accounts and sync
		{pattern: "GET /api/banks", handler: s.banks},
		{pattern: "GET /api/accounts", handler: s.accounts},
		{pattern: "PATCH /api/accounts/{id}", handler: s.patchAccount},
		{pattern: "GET /api/connections", handler: s.connections},
		{pattern: "POST /api/connections", handler: s.startConnection},
		{pattern: "GET " + callbackPath, handler: s.callback},
		{pattern: "DELETE /api/connections/{id}", handler: s.deleteConnection},
		{pattern: "POST /api/sync", handler: s.triggerSync},
		{pattern: "GET /api/sync", handler: s.syncStatus},

		// Property management: overview and reports
		{pattern: "GET /api/hv/meta", handler: s.hvMeta},
		{pattern: "GET /api/hv/overview", handler: s.hvOverview},
		{pattern: "GET /api/hv/report", handler: s.hvReport},
		{pattern: "GET /api/hv/nk", handler: s.hvNK},

		// Property management: properties, units, leases
		{pattern: "POST /api/hv/properties", handler: s.hvSaveProperty},
		{pattern: "PUT /api/hv/properties/{id}", handler: s.hvSaveProperty},
		{pattern: "DELETE /api/hv/properties/{id}", handler: s.hvDelete(s.st.DeleteProperty)},
		{pattern: "POST /api/hv/units", handler: s.hvSaveUnit},
		{pattern: "PUT /api/hv/units/{id}", handler: s.hvSaveUnit},
		{pattern: "DELETE /api/hv/units/{id}", handler: s.hvDelete(s.st.DeleteUnit)},
		{pattern: "GET /api/hv/leases/{id}", handler: s.hvLease},
		{pattern: "POST /api/hv/leases", handler: s.hvSaveLease},
		{pattern: "PUT /api/hv/leases/{id}", handler: s.hvSaveLease},
		{pattern: "DELETE /api/hv/leases/{id}", handler: s.hvDelete(s.st.DeleteLease)},
		{pattern: "POST /api/hv/leases/{id}/steps", handler: s.hvAddStep},
		{pattern: "DELETE /api/hv/steps/{id}", handler: s.hvDelete(s.st.DeleteRentStep)},

		// Property management: rent account and utility costs
		{pattern: "GET /api/hv/transactions", handler: s.hvTransactions},
		{pattern: "PATCH /api/hv/transactions/{id}", handler: s.hvPatchTxn},
		{pattern: "POST /api/hv/manual-costs", handler: s.hvAddManualCost},
		{pattern: "DELETE /api/hv/manual-costs/{id}", handler: s.hvDelete(s.st.DeleteManualCost)},
		{pattern: "PUT /api/hv/nk-keys", handler: s.hvSetKey},

		// Property management: reminders
		{pattern: "POST /api/hv/reminders", handler: s.hvAddReminder},
		{pattern: "PATCH /api/hv/reminders/{id}", handler: s.hvReminderDone},
		{pattern: "DELETE /api/hv/reminders/{id}", handler: s.hvDelete(s.st.DeleteReminder)},
	}
}
