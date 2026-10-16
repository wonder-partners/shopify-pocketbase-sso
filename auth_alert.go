package shopifysso

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/routine"
)

// Mark only this integration's auth transactions. Other auth methods keep
// PocketBase's normal alert delivery behavior, even if the host wraps them in
// its own transaction.
type shopifyAuthTransaction struct {
	core.App
	original core.App
}

func deferShopifyAuthAlert(e *core.MailerRecordEvent) error {
	tx, ok := e.App.(*shopifyAuthTransaction)
	if !ok {
		return e.Next()
	}
	// The mail event owns its message and remaining hook chain. Detach its
	// record from the auth response and give deferred hooks the nontransactional
	// app, so they can read and write after the transaction has completed.
	e.Record = e.Record.Clone()
	e.App = tx.original
	tx.TxInfo().OnComplete(func(txErr error) error {
		if txErr == nil {
			routine.FireAndForget(func() {
				if err := e.Next(); err != nil {
					// SMTP errors may contain addresses or server credentials.
					e.App.Logger().Warn("Unable to send the Shopify login alert")
				}
			})
		}
		return nil
	})
	return nil
}
