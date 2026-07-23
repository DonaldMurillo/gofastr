package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
)

type recordSummaryViewportMetrics struct {
	ViewportWidth    float64 `json:"viewportWidth"`
	DocumentWidth    float64 `json:"documentWidth"`
	MobileMedia      bool    `json:"mobileMedia"`
	LayoutWidth      float64 `json:"layoutWidth"`
	LayoutRight      float64 `json:"layoutRight"`
	LayoutDisplay    string  `json:"layoutDisplay"`
	LayoutColumns    string  `json:"layoutColumns"`
	ContentWidth     float64 `json:"contentWidth"`
	ContentRight     float64 `json:"contentRight"`
	DocWidth         float64 `json:"docWidth"`
	DocRight         float64 `json:"docRight"`
	DocClient        float64 `json:"docClient"`
	DocScroll        float64 `json:"docScroll"`
	DocDisplay       string  `json:"docDisplay"`
	DocCSSWidth      string  `json:"docCSSWidth"`
	DocMinWidth      string  `json:"docMinWidth"`
	DocMaxWidth      string  `json:"docMaxWidth"`
	DocBoxSizing     string  `json:"docBoxSizing"`
	DocPadding       string  `json:"docPadding"`
	DocChildWidth    float64 `json:"docChildWidth"`
	DocChildTag      string  `json:"docChildTag"`
	SummaryWidth     float64 `json:"summaryWidth"`
	SummaryRight     float64 `json:"summaryRight"`
	SummaryClient    float64 `json:"summaryClient"`
	SummaryScroll    float64 `json:"summaryScroll"`
	WorstChildRight  float64 `json:"worstChildRight"`
	ActionsWidth     float64 `json:"actionsWidth"`
	ActionsTop       float64 `json:"actionsTop"`
	ActionsBottom    float64 `json:"actionsBottom"`
	PrimaryActionEnd float64 `json:"primaryActionEnd"`
	ActionWrap       string  `json:"actionWrap"`
	HighlightTop     float64 `json:"highlightTop"`
	LeadColumns      string  `json:"leadColumns"`
	OverflowBG       string  `json:"overflowBG"`
	InitialClearance float64 `json:"initialClearance"`
	StatusInset      float64 `json:"statusInset"`
	TitleSize        string  `json:"titleSize"`
	MetricColumns    string  `json:"metricColumns"`
	MetricBandWidth  float64 `json:"metricBandWidth"`
	LastMetricWidth  float64 `json:"lastMetricWidth"`
	LastMetricAlign  string  `json:"lastMetricAlign"`
}

func TestE2ERecordSummaryResponsiveContract(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := siteE2EServer(t)
	for _, tc := range []struct {
		name          string
		width, height int64
		wantColumns   int
		maxTitlePX    float64
	}{
		{name: "desktop", width: 1440, height: 1000, wantColumns: 5, maxTitlePX: 40},
		{name: "mobile", width: 390, height: 844, wantColumns: 2, maxTitlePX: 32},
	} {
		for _, scheme := range axetest.Schemes {
			t.Run(tc.name+"/"+scheme, func(t *testing.T) {
				ctx := siteBrowserCtx(t)
				var got recordSummaryViewportMetrics
				var shot, fullShot []byte
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(tc.width, tc.height),
					chromedp.Navigate(base+"/components/recordsummary"),
					chromedp.WaitReady("body", chromedp.ByQuery),
					axetest.Prepare(scheme),
					chromedp.Sleep(100*time.Millisecond),
					chromedp.Evaluate(`(() => {
  const root = document.querySelector('[data-fui-comp="ui-record-summary"]');
  const title = root.querySelector('.ui-record-summary__title');
  const actions = root.querySelector('.ui-record-summary__actions');
  const actionCluster = actions.querySelector('.ui-cluster');
  const primaryAction = actions.querySelector('.ui-button');
  const highlight = root.querySelector('.ui-record-summary__highlight');
  const lead = root.querySelector('.ui-record-summary__lead');
  const overflow = root.querySelector('.ui-avatar-group__overflow');
  const avatars = [...root.querySelectorAll('.ui-avatar-group .ui-avatar')];
  const initialClearance = avatars.slice(1).map((avatar, index) =>
    avatar.querySelector('.ui-avatar__initials').getBoundingClientRect().left - avatars[index].getBoundingClientRect().right);
  const statusInsets = avatars.filter(avatar => avatar.querySelector('.ui-avatar__status')).map(avatar => {
    const avatarRect = avatar.getBoundingClientRect();
    const statusRect = avatar.querySelector('.ui-avatar__status').getBoundingClientRect();
    return Math.min(avatarRect.right - statusRect.right, avatarRect.bottom - statusRect.bottom);
  });
  const band = root.querySelector('[data-fui-comp="ui-metric-band"]');
  const lastMetric = band.querySelector('.ui-metric-band__item:last-child');
  const layout = document.querySelector('.layout-components > .layout-body');
  const content = layout.querySelector('.layout-content');
  const doc = document.querySelector('[data-fui-comp="ui-doc-layout"]');
  const docStyle = getComputedStyle(doc);
  const widestDocChild = [...doc.children].sort((a, b) => b.getBoundingClientRect().width - a.getBoundingClientRect().width)[0];
  const descendants = [root, ...root.querySelectorAll('*')];
  return {
    viewportWidth: window.innerWidth,
    documentWidth: document.documentElement.scrollWidth,
    mobileMedia: matchMedia('(max-width: 900px)').matches,
    layoutWidth: layout.getBoundingClientRect().width,
    layoutRight: layout.getBoundingClientRect().right,
    layoutDisplay: getComputedStyle(layout).display,
    layoutColumns: getComputedStyle(layout).gridTemplateColumns,
    contentWidth: content.getBoundingClientRect().width,
    contentRight: content.getBoundingClientRect().right,
    docWidth: doc.getBoundingClientRect().width,
    docRight: doc.getBoundingClientRect().right,
    docClient: doc.clientWidth,
    docScroll: doc.scrollWidth,
    docDisplay: docStyle.display,
    docCSSWidth: docStyle.width,
    docMinWidth: docStyle.minWidth,
    docMaxWidth: docStyle.maxWidth,
    docBoxSizing: docStyle.boxSizing,
    docPadding: docStyle.padding,
    docChildWidth: widestDocChild.getBoundingClientRect().width,
    docChildTag: widestDocChild.tagName + '.' + widestDocChild.className,
    summaryWidth: root.getBoundingClientRect().width,
    summaryRight: root.getBoundingClientRect().right,
    summaryClient: root.clientWidth,
    summaryScroll: root.scrollWidth,
    worstChildRight: Math.max(...descendants.map(el => el.getBoundingClientRect().right)),
    actionsWidth: actions.getBoundingClientRect().width,
    actionsTop: actions.getBoundingClientRect().top,
    actionsBottom: actions.getBoundingClientRect().bottom,
    primaryActionEnd: primaryAction.getBoundingClientRect().bottom,
    actionWrap: actionCluster ? getComputedStyle(actionCluster).flexWrap : "missing",
    highlightTop: highlight.getBoundingClientRect().top,
    leadColumns: getComputedStyle(lead).gridTemplateColumns,
    overflowBG: getComputedStyle(overflow).backgroundColor,
    initialClearance: Math.min(...initialClearance),
    statusInset: Math.min(...statusInsets),
    titleSize: getComputedStyle(title).fontSize,
    metricColumns: getComputedStyle(band).gridTemplateColumns,
    metricBandWidth: band.getBoundingClientRect().width,
    lastMetricWidth: lastMetric.getBoundingClientRect().width,
    lastMetricAlign: getComputedStyle(lastMetric).textAlign,
  };
					})()`, &got),
					chromedp.CaptureScreenshot(&shot),
					chromedp.FullScreenshot(&fullShot, 100),
				); err != nil {
					t.Fatalf("chromedp: %v", err)
				}

				if got.DocumentWidth > got.ViewportWidth+1 {
					t.Errorf("page overflows horizontally: document=%.1f viewport=%.1f media=%v layout=%.1fx right %.1f display=%s columns=%q content=%.1fx right %.1f doc=%.1fx right %.1f client=%.1f scroll=%.1f display=%s cssWidth=%s min=%s max=%s box=%s pad=%s widestChild=%.1f %s", got.DocumentWidth, got.ViewportWidth, got.MobileMedia, got.LayoutWidth, got.LayoutRight, got.LayoutDisplay, got.LayoutColumns, got.ContentWidth, got.ContentRight, got.DocWidth, got.DocRight, got.DocClient, got.DocScroll, got.DocDisplay, got.DocCSSWidth, got.DocMinWidth, got.DocMaxWidth, got.DocBoxSizing, got.DocPadding, got.DocChildWidth, got.DocChildTag)
				}
				fullConfig, err := png.DecodeConfig(bytes.NewReader(fullShot))
				if err != nil {
					t.Fatalf("decode full screenshot: %v", err)
				}
				if fullConfig.Width != int(tc.width) {
					t.Errorf("full screenshot exposes visual horizontal overflow: width=%d viewport=%d", fullConfig.Width, tc.width)
				}
				if got.SummaryScroll > got.SummaryClient+1 {
					t.Errorf("RecordSummary content overflows its box: scroll=%.1f client=%.1f", got.SummaryScroll, got.SummaryClient)
				}
				if got.SummaryRight > got.ViewportWidth+1 || got.WorstChildRight > got.ViewportWidth+1 {
					t.Errorf("RecordSummary is clipped: summary right=%.1f child right=%.1f viewport=%.1f", got.SummaryRight, got.WorstChildRight, got.ViewportWidth)
				}
				if got.ActionsWidth >= got.SummaryWidth*.9 {
					t.Errorf("action row stretched: actions=%.1f summary=%.1f", got.ActionsWidth, got.SummaryWidth)
				}
				if got.ActionWrap != "wrap" {
					t.Errorf("nested Cluster in Actions must wrap whole controls: flex-wrap=%q", got.ActionWrap)
				}
				leadColumns := len(strings.Fields(got.LeadColumns))
				if tc.name == "desktop" && leadColumns != 2 {
					t.Errorf("RecordSummary support rail columns = %d (%q), want 2", leadColumns, got.LeadColumns)
				}
				if tc.name == "mobile" {
					if leadColumns != 1 {
						t.Errorf("RecordSummary mobile lead columns = %d (%q), want 1", leadColumns, got.LeadColumns)
					}
					if got.ActionsTop >= got.HighlightTop {
						t.Errorf("primary action must precede the longer highlight on mobile: action top=%.1f highlight top=%.1f", got.ActionsTop, got.HighlightTop)
					}
					if got.PrimaryActionEnd > float64(tc.height) {
						t.Errorf("primary action falls below the first mobile viewport: bottom=%.1f viewport=%d", got.PrimaryActionEnd, tc.height)
					}
				}
				if scheme == "dark" && got.OverflowBG == "rgb(229, 229, 229)" {
					t.Errorf("AvatarGroup overflow chip fell back to the light-only color in dark mode: %s", got.OverflowBG)
				}
				if got.InitialClearance < -0.5 {
					t.Errorf("AvatarGroup overlap clips identity initials: clearance=%.1fpx", got.InitialClearance)
				}
				if got.StatusInset < 0.5 {
					t.Errorf("avatar presence dot is not inset inside the avatar corner: inset=%.1fpx", got.StatusInset)
				}
				titlePX, err := strconv.ParseFloat(strings.TrimSuffix(got.TitleSize, "px"), 64)
				if err != nil || titlePX > tc.maxTitlePX {
					t.Errorf("title size = %q, want <= %.0fpx", got.TitleSize, tc.maxTitlePX)
				}
				if columns := len(strings.Fields(got.MetricColumns)); columns != tc.wantColumns {
					t.Errorf("MetricBand columns = %d (%q), want %d", columns, got.MetricColumns, tc.wantColumns)
				}
				if tc.name == "mobile" {
					if got.LastMetricWidth < got.MetricBandWidth-1 {
						t.Errorf("odd final MetricBand item must span the phone row: item=%.1f band=%.1f", got.LastMetricWidth, got.MetricBandWidth)
					}
					if got.LastMetricAlign != "center" {
						t.Errorf("odd final MetricBand item alignment = %q, want center", got.LastMetricAlign)
					}
				}

				if dir := os.Getenv("GOFASTR_VISUAL_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, fmt.Sprintf("recordsummary-%s-%s.png", tc.name, scheme))
					if err := os.WriteFile(path, shot, 0o644); err != nil {
						t.Fatal(err)
					}
					t.Logf("wrote %s", path)
					fullPath := filepath.Join(dir, fmt.Sprintf("recordsummary-%s-%s-full.png", tc.name, scheme))
					if err := os.WriteFile(fullPath, fullShot, 0o644); err != nil {
						t.Fatal(err)
					}
					t.Logf("wrote %s", fullPath)
				}
			})
		}
	}
}
