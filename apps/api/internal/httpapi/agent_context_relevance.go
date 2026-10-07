package httpapi

// Relevance is integrated exclusively into runtimeContextPurpose. There is no
// extra HTTP owner, query, budget or selectors route and no authority fallback.
// The pure AIR adapter consumes a filtered DTO; the final native revalidation
// still checks the original complete approval, including filtered-out sources.
