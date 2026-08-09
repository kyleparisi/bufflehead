package ui

// queryPlanningNote returns the standard "design before scanning" instruction
// block appended to every AI prompt. It tells the agent to plan queries using
// schema/catalog metadata first when large extractions (>500 rows) are needed,
// rather than blindly paginating through 100-row pages.
func queryPlanningNote() string {
	return `
QUERY PLANNING — design before scanning:
For small explorations (≤500 rows), direct queries are fine.
If your analysis requires extracting MORE than 500 rows, STOP and plan first:
1. Study the schema above to understand table semantics, relationships, column types, and any temporal dimensions (e.g. process_date snapshots from ETL loads).
2. Use catalog/metadata queries if required (e.g. SELECT column_name, data_type FROM information_schema.columns WHERE table_name = '...') to discover indexes, partitions, and cardinality before touching data.
3. Design a ranked plan of targeted, grouped queries (aggregations, COUNT DISTINCTs, filtered JOINs on indexed/partition columns) rather than blindly fetching pages of 100 rows.
4. Only execute data-scanning queries after you have a clear analysis plan and know which columns, filters, and groupings matter.
`
}

// queryPlanningNoteBigQuery is the BigQuery-specific variant that emphasizes
// INFORMATION_SCHEMA and partition-aware planning.
func queryPlanningNoteBigQuery() string {
	return `
QUERY PLANNING — design before scanning:
For small explorations (≤500 rows), direct queries are fine.
If your analysis requires extracting MORE than 500 rows, STOP and plan first:
1. Study the schema above to understand table semantics, relationships, column types, and partitioning.
2. Use INFORMATION_SCHEMA queries first to discover table structure, partition columns, and clustering keys before touching data tables — this costs nothing.
3. Design a ranked plan of targeted, grouped queries (aggregations, COUNT DISTINCTs, filtered on partition columns) rather than blindly fetching pages of 100 rows. Remember: LIMIT does NOT reduce bytes scanned.
4. Only execute data-scanning queries after you have a clear analysis plan and know which columns, partition filters, and groupings matter.
5. For snapshot/ETL tables (tables loaded periodically with a process_date or load_date column), always filter to the latest snapshot to avoid double-counting.
`
}
