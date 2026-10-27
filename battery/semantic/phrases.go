package semantic

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonDocIdIsRequired     = i18n.Define("semantic.doc_id_is_required", "doc id is required")
	reasonDocumentsRequired   = i18n.Define("semantic.documents_required", "documents is required and must be non-empty")
	reasonQueryTextIsRequired = i18n.Define("semantic.query_text_is_required", "query.text is required")
	reasonDocumentUnchunkable = i18n.Define("semantic.document_unchunkable", "a document could not be split into chunks")
)
