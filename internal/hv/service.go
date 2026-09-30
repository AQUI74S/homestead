package hv

import (
	"context"
	"time"

	"github.com/AQUI74S/homestead/internal/store"
)

// Reassign re-assigns all transactions of the property management accounts (manual ones are kept).
func Reassign(ctx context.Context, st *store.Store) error {
	txns, err := st.HVTransactions(ctx, time.Time{}, time.Time{})
	if err != nil || len(txns) == 0 {
		return err
	}
	leases, err := st.Leases(ctx)
	if err != nil {
		return err
	}
	props, err := st.Properties(ctx)
	if err != nil {
		return err
	}
	rules, err := st.HVRules(ctx)
	if err != nil {
		return err
	}
	own, err := st.OwnIBANs(ctx)
	if err != nil {
		return err
	}
	return st.ApplyHVAssignments(ctx, Assign(AssignInput{Txns: txns, Leases: leases, Properties: props, Rules: rules, OwnIBANs: own}))
}
