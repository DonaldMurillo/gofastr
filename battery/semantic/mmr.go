package semantic

func mmr(qv []float32, hits []Hit, lambda float64, k int) []Hit {
	if k <= 0 || len(hits) == 0 {
		return nil
	}
	if k > len(hits) {
		k = len(hits)
	}
	if lambda <= 0 {
		out := make([]Hit, k)
		copy(out, hits[:k])
		return out
	}
	if lambda > 1 {
		lambda = 1
	}

	relevance := make([]float64, len(hits))
	for i := range hits {
		if hits[i].Chunk.Vec != nil {
			relevance[i] = float64(dot(qv, hits[i].Chunk.Vec))
		} else {
			relevance[i] = hits[i].Score
		}
	}

	picked := make([]bool, len(hits))
	out := make([]Hit, 0, k)
	for step := 0; step < k; step++ {
		bestIdx := -1
		bestScore := -1.0e18
		for i := range hits {
			if picked[i] {
				continue
			}
			maxSim := 0.0
			if hits[i].Chunk.Vec != nil {
				compared := false
				for _, p := range out {
					if p.Chunk.Vec == nil {
						continue
					}
					sim := float64(dot(hits[i].Chunk.Vec, p.Chunk.Vec))
					if !compared || sim > maxSim {
						maxSim = sim
					}
					compared = true
				}
			}
			score := (1-lambda)*relevance[i] - lambda*maxSim
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			break
		}
		picked[bestIdx] = true
		h := hits[bestIdx]
		h.Score = bestScore
		h.Reason = "mmr"
		out = append(out, h)
	}
	return out
}
