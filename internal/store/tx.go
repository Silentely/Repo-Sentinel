package store

import "context"

// WithTx 在同一事务绑定的 Store 上执行回调。
func (s *storeImpl) WithTx(ctx context.Context, callback func(Store) error) (err error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return errDatabaseOperation
	}
	txStore := newStore(tx.Client(), func() error { return nil })
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
	}()
	if err := callback(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return errDatabaseOperation
	}
	return nil
}
