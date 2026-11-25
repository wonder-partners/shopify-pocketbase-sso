package shopifysso

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func (s *Service) respondToOAuth(e *core.RequestEvent) error {
	return respondAfterCommit(e, commitRules{acceptMFAChallenge: true}, func(tx core.App) error {
		authTx := &shopifyAuthTransaction{App: tx, original: s.app}
		e.App = authTx
		err := e.Next()
		if !e.Written() && authTx.preparedResponse != nil {
			if writeErr := authTx.preparedResponse.sendTo(e.Response); writeErr != nil {
				return writeErr
			}
		}
		return err
	})
}

func shopifyAuthTransactionOf(app core.App) (*shopifyAuthTransaction, bool) {
	switch a := app.(type) {
	case *shopifyAuthTransaction:
		return a, true
	case *authResponseApp:
		return a.transaction, true
	}
	return nil, false
}

func (tx *shopifyAuthTransaction) afterCommit(task func(app core.App)) {
	tx.TxInfo().OnComplete(func(txErr error) error {
		if txErr == nil {
			task(tx.original)
		}
		return nil
	})
}

type shopifyAuthTransaction struct {
	core.App
	original         core.App
	preparedResponse *authResponseBuffer
}

func (tx *shopifyAuthTransaction) RunInTransaction(fn func(core.App) error) error {
	return tx.App.RunInTransaction(func(core.App) error { return fn(tx) })
}

func (tx *shopifyAuthTransaction) OnRecordAuthRequest(tags ...string) *hook.TaggedHook[*core.RecordAuthRequestEvent] {
	local := &hook.Hook[*core.RecordAuthRequestEvent]{}
	local.BindFunc(func(e *core.RecordAuthRequestEvent) error {
		finalizer := e.Event
		return tx.App.OnRecordAuthRequest(tags...).Trigger(e, func(e *core.RecordAuthRequestEvent) error {
			if e.Written() {
				return finalizer.Next()
			}
			originalApp, originalResponse := e.App, e.Response
			prepared := &authResponseBuffer{header: originalResponse.Header().Clone()}
			e.App = &authResponseApp{App: originalApp, transaction: tx}
			e.Response = prepared
			defer func() { e.App, e.Response = originalApp, originalResponse }()
			err := finalizer.Next()
			if (err == nil || err == apis.ErrMFA) && prepared.Written() {
				tx.preparedResponse = prepared
			}
			return err
		})
	})
	return hook.NewTaggedHook(local, tags...)
}

// Only the native response finalizer sees TxInfo=nil, which makes PocketBase
// serialize immediately. Promoted DB methods still use the actual transaction.
// Project enrich hooks receive the original app with its real transaction info.
type authResponseApp struct {
	core.App
	transaction *shopifyAuthTransaction
}

func (a *authResponseApp) TxInfo() *core.TxAppInfo { return nil }

func (a *authResponseApp) OnRecordEnrich(tags ...string) *hook.TaggedHook[*core.RecordEnrichEvent] {
	local := &hook.Hook[*core.RecordEnrichEvent]{}
	local.BindFunc(func(e *core.RecordEnrichEvent) error {
		finalizer := e.Event
		original := e.App
		e.App = a.App
		defer func() { e.App = original }()
		return a.App.OnRecordEnrich(tags...).Trigger(e, func(e *core.RecordEnrichEvent) error {
			return finalizer.Next()
		})
	})
	return hook.NewTaggedHook(local, tags...)
}
