package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestCopySkipsRevealedPassword(t *testing.T) {
	field := Password(PasswordProps{Name: "credential", ID: "code"}, nil)
	ctx := behaviorPage(t, startBehaviorServer(t, string(field)+copyControl))
	if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr(BehaviorName)) {
		t.Fatal("copy or reveal module never loaded")
	}
	var got struct {
		Type   string
		Writes int
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		let writes = 0;
		Object.defineProperty(navigator, 'clipboard', {value: {writeText: () => { writes++; return Promise.resolve(); }}});
		const input = document.getElementById('code');
		const copy = document.getElementById('cp');
		input.value = 'dummy';
		copy.click();
		document.querySelector('[data-hui-reveal]').click();
		copy.click();
		return {type: input.type, writes};
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	if got.Type != "text" {
		t.Fatalf("reveal left input type %q, want text", got.Type)
	}
	if got.Writes != 0 {
		t.Fatalf("clipboard got %d writes across concealed and revealed states, want none", got.Writes)
	}
}
