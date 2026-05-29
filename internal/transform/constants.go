package transform

// Transformer name constants.
const (
	transformerNameFilter           = "filter"
	transformerNameLinkExtraction   = "link_extraction"
	transformerNameSignatureRemoval = "signature_removal"
	transformerNameThreadGrouping   = "thread_grouping"
)

// Link type constants used by LinkExtractionTransformer.
const (
	linkTypeExternal = "external"
	linkTypeDocument = "document"
)

// AI analysis backend constants.
const (
	backendCLI  = "cli"
	backendHTTP = "http"
)

// Thread mode constants.
const (
	threadModeSummary = "summary"
)
