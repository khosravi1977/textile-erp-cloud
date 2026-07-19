package postgres

import (
	"strings"
	"testing"
)

func TestSplitSQLStatementsKeepsDollarQuotedFunctionTogether(t *testing.T) {
	input := `
-- comment before function
CREATE OR REPLACE FUNCTION demo()
RETURNS void AS $$
BEGIN
    RAISE NOTICE 'hello; still inside function';
END;
$$ LANGUAGE plpgsql;

CREATE TABLE demo_table (id BIGINT);
`

	statements := splitSQLStatements(input)
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(statements), statements)
	}
	if !strings.Contains(statements[0], "RAISE NOTICE") {
		t.Fatalf("function body was split incorrectly: %q", statements[0])
	}
	if !strings.Contains(statements[1], "CREATE TABLE demo_table") {
		t.Fatalf("second statement mismatch: %q", statements[1])
	}
}

func TestNormalizeSQLStatementRemovesLeadingComments(t *testing.T) {
	stmt := normalizeSQLStatement(`
-- leading comment
CREATE TABLE demo_table (id BIGINT);
`)

	if strings.HasPrefix(stmt, "--") {
		t.Fatalf("expected leading comment to be removed: %q", stmt)
	}
	if !strings.HasPrefix(stmt, "CREATE TABLE") {
		t.Fatalf("expected executable SQL after normalization: %q", stmt)
	}
}
