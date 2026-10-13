package render

import (
	htmltemplate "html/template"
	"strings"
	"testing"
)

// TestTableContextNoTableTypedNilMap pins tableContext's no-table shape
// (table-layout-context, gfm-tables): a body carrying no table yields table as
// a TYPED nil map (map[string]any(nil)), not a bare nil interface, and tables as
// a non-nil but empty list. Executing the documented AC shape
// `{{ range .table.rows }}x{{ end }}{{ len .tables }}` over the pair must render
// "0" with no error: `.table.rows` is an absent-key lookup on a nil map that
// ranges as a no-op, while `.tables` measures the empty list — a layout reads
// both entries without a guard.
func TestTableContextNoTableTypedNilMap(t *testing.T) {
	table, tables := tableContext("", nil)

	tm, ok := table.(map[string]any)
	if !ok {
		t.Fatalf("tableContext(no-table) table = %T, want map[string]any", table)
	}
	if tm != nil {
		t.Errorf("tableContext(no-table) table = %#v, want a typed nil map", tm)
	}
	if len(tm) != 0 {
		t.Errorf("tableContext(no-table) len(table) = %d, want 0", len(tm))
	}
	if tables == nil {
		t.Error("tableContext(no-table) tables = nil, want a non-nil empty list")
	} else if len(tables) != 0 {
		t.Errorf("tableContext(no-table) tables = %v, want an empty list", tables)
	}

	tmpl, err := htmltemplate.New("t").Parse(`{{ range .table.rows }}x{{ end }}{{ len .tables }}`)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, map[string]any{"table": table, "tables": tables}); err != nil {
		t.Fatalf(`Execute() error = %v, want nil: {{ range .table.rows }} must be a no-op on a typed nil table`, err)
	}
	if got := sb.String(); got != "0" {
		t.Errorf("Execute() = %q, want %q", got, "0")
	}
}
