package check

import "testing"

func TestNumberFillChecksCountRatios(t *testing.T) {
	dir := writeRuntimeFixture(t, "ratio.js", `
count.textContent = q ? shown + '/' + total : String(total);
count.textContent = shown + '/' + NS.formatNumber(total);
count.textContent = NS.formatNumber(shown) + '/' + NS.formatNumber(total);
count.textContent = '1/2';
`)
	result, err := LintNumberFill(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Violations) != 2 {
		t.Fatalf("count-ratio findings = %d, want 2: %s", len(result.Violations), result.Error())
	}
}
