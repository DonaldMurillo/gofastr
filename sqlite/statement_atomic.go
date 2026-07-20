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
	// the in-memory pager state (page cache + dirty bits + the transaction's
	// pre-BEGIN originals) so a failed statement rolls back to the statement
	// boundary without discarding earlier successful work in the transaction.
	// StatementSnapshot must NOT touch the on-disk file: the outer
	// transaction's eventual RollbackTxn assumes the file still reflects the
	// pre-BEGIN state.
	pagerSnapshot := e.pager.StatementSnapshot()
	result, err := e.executeMutation(stmt, parsed, params)
	if err == nil {
		return result, nil
	}
	e.pager.StatementRestore(pagerSnapshot)
	e.schema = schemaSnapshot
	return nil, err
}
