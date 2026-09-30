package syncer

import (
	"context"
	"fmt"
	"time"

	"github.com/AQUI74S/homestead/internal/domain"
	eb "github.com/AQUI74S/homestead/internal/enablebanking"
	"github.com/AQUI74S/homestead/internal/store"
)

// defaultConsent is assumed when the bank does not say how long the consent is valid.
const defaultConsent = 90 * 24 * time.Hour

// defaultAccountName is used when the bank provides neither a name nor a product.
const defaultAccountName = "Konto"

// CompleteAuth completes the bank authorization: create the session, store accounts, start the first sync.
// psu (the user who just returned from the bank) makes the first sync user-initiated.
func (s *Syncer) CompleteAuth(ctx context.Context, state, code string, psu *eb.PSU) (*store.Connection, error) {
	conn, err := s.st.ConnectionByState(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("unbekannte oder bereits verwendete Freigabe: %w", err)
	}
	sess, err := s.p.CreateSession(ctx, code)
	if err != nil {
		_ = s.st.SetConnectionStatus(ctx, conn.ID, domain.ConnError, err.Error())
		return nil, err
	}
	valid := sess.Access.ValidUntil
	if valid.IsZero() {
		valid = s.now().Add(defaultConsent)
	}
	if err := s.st.ActivateConnection(ctx, conn.ID, sess.SessionID, valid); err != nil {
		return nil, err
	}
	for _, a := range sess.Accounts {
		name := a.Name
		if name == "" {
			name = a.Product
		}
		if name == "" {
			name = defaultAccountName
		}
		id := conn.ID
		if _, err := s.st.UpsertAccount(ctx, store.Account{ConnectionID: &id, ProviderUID: a.UID, IBAN: a.AccountID.IBAN,
			BankName: conn.ASPSPName, Name: name, Currency: a.Currency, Book: conn.Book}); err != nil {
			return nil, err
		}
	}
	s.TriggerAsync(psu)
	return conn, nil
}
