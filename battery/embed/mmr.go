package embed

// mmr applies Maximal Marginal Relevance reranking to candidates,
// returning the top k items balanced between relevance (relevance to
// the query) and diversity (dissimilarity from already-picked items).
//
// lambda ∈ [0,1]: 0 collapses to plain relevance ordering; 1 picks the
// most diverse-from-already-picked items regardless of query
// relevance. Typical useful range is 0.2–0.5.
//
// All hits MUST carry vectors (Chunk.Vec). The query vector qv MUST be
// L2-normalized.
func mmr(qv []float32, hits []Hit, lambda float64, k int) []Hit {
	if k <= 0 || len(hits) == 0 {
		return nil
	}
	if k > len(hits) {
		k = len(hits)
	}
	// Defensive: when lambda is 0 we'd produce a no-op anyway, so bail.
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
			// Fall back to the existing score so chunks lacking a stored
			// vector (e.g. coming purely from the keyword backend) still
			// participate.
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
				for _, p := range out {
					if p.Chunk.Vec == nil {
						continue
					}
					sim := float64(dot(hits[i].Chunk.Vec, p.Chunk.Vec))
					if sim > maxSim {
						maxSim = sim
					}
				}
			}
			score := lambda*relevance[i] - (1-lambda)*maxSim
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
