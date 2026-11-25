package shopifysso

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/routine"
)

func deferShopifyAuthAlert(e *core.MailerRecordEvent) error {
	tx, ok := shopifyAuthTransactionOf(e.App)
	if !ok {
		return e.Next()
	}
	e.Record = e.Record.Clone()
	tx.afterCommit(func(app core.App) {
		e.App = app
		routine.FireAndForget(func() {
			if err := e.Next(); err != nil {
				e.App.Logger().Warn("Unable to send the Shopify login alert")
			}
		})
	})
	return nil
}
