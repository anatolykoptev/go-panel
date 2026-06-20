package semantic

import (
	"strings"
	"testing"
)

// ptrBool is a helper to get a pointer to a bool literal.
func ptrBool(v bool) *bool { return &v }

// TestBuildSQL_ConfigurableFilterColumns is the hardcode-extraction guard.
//
// It proves that buildSQL emits the SOURCE-CONFIGURED column names
// (PodborkaColumn / SegmentColumn) rather than hardcoded literals
// "is_podborka" / "segment".
//
// RED phase (before the GREEN fix): buildSQL still emits the literals
// "is_podborka" and "segment" regardless of Source field values, so this
// test fails with messages like:
//
//	sql does not contain configured PodborkaColumn "is_collection"
//	sql contains hardcoded literal "is_podborka" (should use src.PodborkaColumn)
//
// GREEN phase (after fix): buildSQL emits "is_collection" and "tier",
// and the literals "is_podborka" / "segment" are absent.
//
// Falsification check: revert the buildSQL fix (restore the hardcoded
// literals) and this test turns RED again -- confirming it guards the
// production change.
func TestBuildSQL_ConfigurableFilterColumns(t *testing.T) {
	// Use deliberately non-default column names to distinguish configured
	// names from hardcoded literals.
	src := Source{
		Name:           "custom_source",
		Table:          "custom_table",
		IDColumn:       "row_id",
		VecColumn:      "embedding",
		KindConst:      "article",
		PodborkaColumn: "is_collection", // NOT "is_podborka"
		SegmentColumn:  "tier",          // NOT "segment"
	}

	vec := make([]float32, 4)
	for i := range vec {
		vec[i] = 0.5
	}

	// NIT: inline ptrBool(true) directly instead of an isPodborka local used once.
	f := Filters{
		IsPodborka: ptrBool(true),
		Segment:    "premium",
	}

	sql, _ := buildSQL(src, vec, 10, f)

	// Assert configured column names appear.
	if !strings.Contains(sql, "is_collection") {
		t.Errorf("sql does not contain configured PodborkaColumn %q\nsql: %s", src.PodborkaColumn, sql)
	}
	if !strings.Contains(sql, "tier") {
		t.Errorf("sql does not contain configured SegmentColumn %q\nsql: %s", src.SegmentColumn, sql)
	}

	// Assert hardcoded literals are absent.
	if strings.Contains(sql, "is_podborka") {
		t.Errorf("sql contains hardcoded literal %q; should use src.PodborkaColumn=%q instead\nsql: %s",
			"is_podborka", src.PodborkaColumn, sql)
	}
	// NIT: symmetric single check, mirroring the is_podborka guard above.
	// The configured SegmentColumn is "tier", so "segment" can only appear as
	// a hardcoded literal -- no false positive.
	if strings.Contains(sql, "segment") {
		t.Errorf("sql contains hardcoded literal %q; should use src.SegmentColumn=%q instead\nsql: %s",
			"segment", src.SegmentColumn, sql)
	}
}
