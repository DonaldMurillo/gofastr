package sqlite

import "fmt"

func (e *Engine) executeMutationAtomic(stmt Statement, parsed Statement, params []Value) (*Result, error) {
	schemaSnapshot := e.schema.Copy()
	if e.txnSnap == nil {
		e.pager.BeginTxn()
		result, err := e.executeMutation(stmt, parsed, params)
		if err == nil {
			e.pager.CommitTxn()
			return result, nil
		}
		if rollbackErr := e.pager.RollbackTxn(); rollbackErr != nil {
			return nil, fmt.Errorf("%w (statement rollback failed: %v)", err, rollbackErr)
		}
		e.schema = schemaSnapshot
		return nil, err
	}

	// An explicit transaction already owns the pager's COW journal. Snapshot
	// its current state so a failed statement rolls back to the statement
	// boundary without discarding earlier successful work in the transaction.
	pagerSnapshot := e.pager.Snapshot()
	result, err := e.executeMutation(stmt, parsed, params)
	if err == nil {
		return result, nil
	}
	if restoreErr := e.pager.Restore(pagerSnapshot); restoreErr != nil {
		return nil, fmt.Errorf("%w (statement rollback failed: %v)", err, restoreErr)
	}
	e.schema = schemaSnapshot
	return nil, err
}
