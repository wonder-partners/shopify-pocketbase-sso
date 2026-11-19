package shopifysso

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/routine"
)

func deferShopifyAuthAlert(e *core.MailerRecordEvent) error {
	login, ok := shopifyLoginOf(e.App)
	if !ok {
		return e.Next()
	}
	// The mail event owns its message and remaining hook chain. Detach its
	// record from the auth response; deferred hooks get the nontransactional
	// app, so they can read and write after the login has committed.
	e.Record = e.Record.Clone()
	login.afterCommit(func(app core.App) {
		e.App = app
		routine.FireAndForget(func() {
			if err := e.Next(); err != nil {
				// SMTP errors may contain addresses or server credentials.
				e.App.Logger().Warn("Unable to send the Shopify login alert")
			}
		})
	})
	return nil
}
