package evalrunner

import (
	"fmt"
	"sort"
	"strings"
)

var dimensionWeights = map[string]float64{
	"hierarchy":           0.15,
	"composition":         0.15,
	"typography":          0.12,
	"product_specificity": 0.13,
	"density":             0.10,
	"component_polish":    0.12,
	"responsive_intent":   0.13,
	"theme_coherence":     0.10,
}

func aggregateJudgments(judgments []Judgment) (Dimensions, float64, float64) {
	if len(judgments) == 0 {
		return Dimensions{}, 0, 0
	}
	values := func(pick func(Dimensions) float64) float64 {
		v := make([]float64, 0, len(judgments))
		for _, judgment := range judgments {
			v = append(v, pick(judgment.Dimensions))
		}
		return median(v)
	}
	d := Dimensions{
		Hierarchy:          values(func(d Dimensions) float64 { return d.Hierarchy }),
		Composition:        values(func(d Dimensions) float64 { return d.Composition }),
		Typography:         values(func(d Dimensions) float64 { return d.Typography }),
		ProductSpecificity: values(func(d Dimensions) float64 { return d.ProductSpecificity }),
		Density:            values(func(d Dimensions) float64 { return d.Density }),
		ComponentPolish:    values(func(d Dimensions) float64 { return d.ComponentPolish }),
		ResponsiveIntent:   values(func(d Dimensions) float64 { return d.ResponsiveIntent }),
		ThemeCoherence:     values(func(d Dimensions) float64 { return d.ThemeCoherence }),
	}
	overall :=
		d.Hierarchy*dimensionWeights["hierarchy"] +
			d.Composition*dimensionWeights["composition"] +
			d.Typography*dimensionWeights["typography"] +
			d.ProductSpecificity*dimensionWeights["product_specificity"] +
			d.Density*dimensionWeights["density"] +
			d.ComponentPolish*dimensionWeights["component_polish"] +
			d.ResponsiveIntent*dimensionWeights["responsive_intent"] +
			d.ThemeCoherence*dimensionWeights["theme_coherence"]
	minimum := minDimension(d)
	return d, overall, minimum
}

func minDimension(d Dimensions) float64 {
	values := []float64{d.Hierarchy, d.Composition, d.Typography, d.ProductSpecificity, d.Density, d.ComponentPolish, d.ResponsiveIntent, d.ThemeCoherence}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func candidateMeetsQualityBar(suite *Suite, result CandidateResult, holisticRuns, mobileRuns int) bool {
	consensusPassed := !suite.Judge.RequireShadcnConsensus || (result.HolisticShadcnConsensus && result.MobileShadcnConsensus)
	return result.TechnicalPassed &&
		len(result.Judgments) == holisticRuns && len(result.MobileJudgments) == mobileRuns &&
		result.Overall >= suite.Judge.MinimumOverall && result.MinimumDimension >= suite.Judge.MinimumDimension &&
		result.MobileOverall >= suite.Judge.MinimumMobileOverall && result.MobileMinimumDimension >= suite.Judge.MinimumMobileDimension &&
		consensusPassed
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	m := len(v) / 2
	if len(v)%2 == 1 {
		return v[m]
	}
	return (v[m-1] + v[m]) / 2
}

func summarize(suite *Suite, runID string, candidates []CandidateResult, nonCompetitiveReasons []string) Summary {
	byVariant := map[string][]CandidateResult{}
	for _, candidate := range candidates {
		byVariant[candidate.VariantID] = append(byVariant[candidate.VariantID], candidate)
	}
	var variants []VariantSummary
	for _, configured := range suite.Variants {
		cells := byVariant[configured.ID]
		if len(cells) == 0 {
			continue
		}
		vs := VariantSummary{
			VariantID: configured.ID, Candidates: len(cells), WorstOverall: 10, WorstMobileOverall: 10,
			AllTechnicalPassed: true, AllQualityPassed: true, PromotionEligible: configured.HistoricalOnlyReason == "",
		}
		var metricCells int64
		for _, cell := range cells {
			if cell.TechnicalPassed {
				vs.TechnicalPassRate++
			} else {
				vs.AllTechnicalPassed = false
			}
			if cell.QualityPassed {
				vs.QualityPassRate++
			} else {
				vs.AllQualityPassed = false
			}
			vs.MeanOverall += cell.Overall
			vs.MeanMinimumDimension += cell.MinimumDimension
			vs.MeanMobileOverall += cell.MobileOverall
			if cell.Overall < vs.WorstOverall {
				vs.WorstOverall = cell.Overall
			}
			if cell.MobileOverall < vs.WorstMobileOverall {
				vs.WorstMobileOverall = cell.MobileOverall
			}
			vs.MeanDocsCalls += float64(cell.BuilderDocsCalls)
			if cell.BuilderUsedCapabilityMap {
				vs.CapabilityMapDiscoveryRate++
			}
			if cell.BuilderDuration > 0 || cell.BuilderTokens > 0 {
				vs.MeanBuilderMinutes += cell.BuilderDuration / 60
				vs.MeanBuilderTokens += cell.BuilderTokens
				metricCells++
			}
		}
		n := float64(len(cells))
		vs.TechnicalPassRate /= n
		vs.QualityPassRate /= n
		vs.MeanOverall /= n
		vs.MeanMinimumDimension /= n
		vs.MeanMobileOverall /= n
		vs.MeanDocsCalls /= n
		vs.CapabilityMapDiscoveryRate /= n
		if metricCells > 0 {
			vs.MeanBuilderMinutes /= float64(metricCells)
			vs.MeanBuilderTokens /= metricCells
		}
		// Reward sustained quality and worst-case strength. Technical failures
		// remain visible and disqualify a winner below.
		vs.RankScore = vs.MeanOverall*0.55 + vs.WorstOverall*0.15 + vs.MeanMobileOverall*0.15 + vs.WorstMobileOverall*0.05 + vs.QualityPassRate*10*0.10
		variants = append(variants, vs)
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].RankScore > variants[j].RankScore })
	summary := Summary{
		Suite: suite.Name, RunID: runID, Competitive: len(nonCompetitiveReasons) == 0, NonCompetitiveReasons: nonCompetitiveReasons,
		QualityBar: QualityBar{
			MinimumOverall: suite.Judge.MinimumOverall, MinimumDimension: suite.Judge.MinimumDimension,
			MinimumMobileOverall: suite.Judge.MinimumMobileOverall, MinimumMobileDimension: suite.Judge.MinimumMobileDimension,
			TechnicalPass: true, ShadcnConsensus: suite.Judge.RequireShadcnConsensus,
		},
		Variants: variants, Candidates: candidates,
	}
	var qualified []VariantSummary
	if summary.Competitive {
		for _, variant := range variants {
			if variant.PromotionEligible && variant.AllTechnicalPassed && variant.AllQualityPassed &&
				variant.MeanOverall >= suite.Judge.MinimumOverall && variant.MeanMinimumDimension >= suite.Judge.MinimumDimension &&
				variant.MeanMobileOverall >= suite.Judge.MinimumMobileOverall {
				qualified = append(qualified, variant)
			}
		}
	}
	if len(qualified) > 0 {
		leaders := tiedTopVariants(qualified)
		if len(leaders) == 1 {
			summary.Winner = leaders[0]
			summary.WinnerMeetsBar = true
		} else {
			summary.TiedLeaders = leaders
			summary.TiedLeadersMeetBar = true
		}
	} else {
		var technical []VariantSummary
		for _, variant := range variants {
			if variant.PromotionEligible && variant.TechnicalPassRate > 0 {
				technical = append(technical, variant)
			}
		}
		leaders := tiedTopVariants(technical)
		if len(leaders) == 1 {
			summary.Winner = leaders[0]
		} else if len(leaders) > 1 {
			summary.TiedLeaders = leaders
		}
	}
	return summary
}

func tiedTopVariants(variants []VariantSummary) []string {
	if len(variants) == 0 {
		return nil
	}
	top := variants[0].RankScore
	var leaders []string
	for _, variant := range variants {
		if variant.RankScore != top {
			break
		}
		leaders = append(leaders, variant.VariantID)
	}
	return leaders
}

func leaderboardMarkdown(summary Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — UI quality leaderboard\n\n", summary.Suite)
	fmt.Fprintf(&b, "Run: `%s`\n\n", summary.RunID)
	if !summary.Competitive {
		fmt.Fprintf(&b, "Noncompetitive run: %s. Scores are diagnostic only.\n\n", strings.Join(summary.NonCompetitiveReasons, "; "))
	}
	b.WriteString("| Rank | Variant | Eligible | Rank score | Holistic | Holistic worst | Mobile | Mobile worst | Mean min dimension | Build min | Build tokens | Capability map | Docs calls | Technical pass | Quality pass |\n")
	b.WriteString("|---:|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for i, v := range summary.Variants {
		fmt.Fprintf(&b, "| %d | %s | %t | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %d | %.0f%% | %.1f | %.0f%% | %.0f%% |\n",
			i+1, v.VariantID, v.PromotionEligible, v.RankScore, v.MeanOverall, v.WorstOverall, v.MeanMobileOverall, v.WorstMobileOverall, v.MeanMinimumDimension,
			v.MeanBuilderMinutes, v.MeanBuilderTokens, v.CapabilityMapDiscoveryRate*100, v.MeanDocsCalls, v.TechnicalPassRate*100, v.QualityPassRate*100)
	}
	b.WriteString("\n")
	if summary.WinnerMeetsBar {
		fmt.Fprintf(&b, "Winner meeting the quality bar: **%s**.\n", summary.Winner)
	} else if summary.TiedLeadersMeetBar {
		fmt.Fprintf(&b, "No unique winner: **%s** are exactly tied and meet the quality bar.\n", strings.Join(summary.TiedLeaders, "**, **"))
	} else if len(summary.TiedLeaders) > 0 {
		fmt.Fprintf(&b, "No unique leader: **%s** are exactly tied.\n", strings.Join(summary.TiedLeaders, "**, **"))
	} else if !summary.Competitive && summary.Winner != "" {
		fmt.Fprintf(&b, "Diagnostic leader: **%s**. This run is noncompetitive and cannot establish a bar-clearing winner.\n", summary.Winner)
	} else if summary.Winner != "" {
		fmt.Fprintf(&b, "Provisional leader: **%s**, but no variant meets the configured shadcn-level quality bar.\n", summary.Winner)
	} else if hasTechnicalEligibleVariant(summary.Variants) {
		b.WriteString("No provisional leader: no promotion-eligible candidate clears the configured quality bar.\n")
	} else {
		b.WriteString("No provisional leader: no promotion-eligible candidate produced a technical pass.\n")
	}
	b.WriteString("\n## Candidate feedback\n\n")
	for _, c := range summary.Candidates {
		fmt.Fprintf(&b, "### %s / %s / run %d\n\n", c.VariantID, c.ScenarioID, c.Repetition)
		if c.HistoricalOnlyReason != "" {
			fmt.Fprintf(&b, "Historical-only framework snapshot: %s This candidate is research evidence only and is not eligible for promotion.\n\n", c.HistoricalOnlyReason)
		}
		fmt.Fprintf(&b, "Holistic %.2f (minimum %.2f); mobile %.2f (minimum %.2f); holistic/mobile shadcn consensus `%t`/`%t`; technical pass `%t`; quality pass `%t`.\n\n",
			c.Overall, c.MinimumDimension, c.MobileOverall, c.MobileMinimumDimension,
			c.HolisticShadcnConsensus, c.MobileShadcnConsensus, c.TechnicalPassed, c.QualityPassed)
		fmt.Fprintf(&b, "Dev-loop funnel (non-deterministic): builder invoked `gofastr dev` `%t`; %d gofastr CLI call(s) total.\n\n",
			c.BuilderUsedDevServer, c.BuilderCLICalls)
		fmt.Fprintf(&b, "Docs funnel (non-deterministic): %d `gofastr docs` call(s); capability map `%t`; topics `%s`; searches `%s`.\n\n",
			c.BuilderDocsCalls, c.BuilderUsedCapabilityMap, strings.Join(c.BuilderDocsTopics, ", "), strings.Join(c.BuilderDocsSearches, ", "))
		fmt.Fprintf(&b, "MCP funnel (non-deterministic): builder touched `/mcp` `%t`; served candidate exposes %d MCP tool(s), introspection `%t`, dev-only log tools leaked into prod boot `%t`.\n\n",
			c.BuilderUsedMCP, c.CandidateMCPTools, c.CandidateMCPIntrospection, c.CandidateMCPLogToolsProd)
		if len(c.Weakest) > 0 {
			b.WriteString("Weakest visible decisions:\n\n")
			for _, item := range c.Weakest {
				fmt.Fprintf(&b, "- %s\n", item)
			}
			b.WriteString("\n")
		}
		if len(c.NextIterations) > 0 {
			b.WriteString("Suggested next iteration:\n\n")
			for _, item := range c.NextIterations {
				fmt.Fprintf(&b, "- %s\n", item)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func hasTechnicalEligibleVariant(variants []VariantSummary) bool {
	for _, variant := range variants {
		if variant.PromotionEligible && variant.TechnicalPassRate > 0 {
			return true
		}
	}
	return false
}
