package controller

import (
	"context"
	"gonum.org/v1/gonum/optimize"
	"gonum.org/v1/gonum/stat/distuv"
	"math"
)

type degreeFit struct {
	Status   string          `json:"status"`
	Method   string          `json:"method"`
	DF       *float64        `json:"df"`
	Location *float64        `json:"location"`
	Scale    *float64        `json:"scale"`
	Density  []analysisPoint `json:"density"`
}

// Fit the observed degree probabilities directly. No synthetic observations,
// integer truncation of weights, or replacement of failed fits by invented data.
func fitDegreeDistribution(ctx context.Context, points []analysisPoint) *degreeFit {
	out := &degreeFit{Status: "insufficient distinct degrees", Method: "weighted Student-t continuous-density MLE; df [0.05,10000], scale >= 0.05 degree", Density: []analysisPoint{}}
	values := []analysisPoint{}
	total, mean, variance := 0., 0., 0.
	for _, p := range points {
		if p.X < 0 || p.Y <= 0 || numberPointer(p.X) == nil || numberPointer(p.Y) == nil {
			continue
		}
		values = append(values, p)
		total += p.Y
		mean += p.X * p.Y
	}
	if len(values) < 3 || total <= 0 {
		return out
	}
	mean /= total
	for _, p := range values {
		variance += (p.X - mean) * (p.X - mean) * p.Y / total
	}
	if variance <= 0 {
		return out
	}
	sd := math.Sqrt(variance)
	problem := optimize.Problem{Func: func(x []float64) float64 {
		if ctx.Err() != nil {
			return math.Inf(1)
		}
		df, scale := math.Exp(x[0]), math.Exp(x[2])
		if df < .05 || df > 10000 || scale < .05 || scale > math.Max(sd, 1)*1000 || math.Abs(x[1]-mean) > math.Max(sd, 1)*1000 {
			return math.Inf(1)
		}
		dist := distuv.StudentsT{Mu: x[1], Sigma: scale, Nu: df}
		cost := 0.
		for _, p := range values {
			cost -= p.Y / total * dist.LogProb(p.X)
		}
		return cost
	}}
	var best *optimize.Result
	for _, initialDF := range []float64{2, 10, 100} {
		if ctx.Err() != nil {
			out.Status = "canceled"
			return out
		}
		result, err := optimize.Minimize(problem, []float64{math.Log(initialDF), mean, math.Log(math.Max(sd, .1))}, &optimize.Settings{FuncEvaluations: 4000, MajorIterations: 2000, Converger: &optimize.FunctionConverge{Absolute: 1e-10, Relative: 1e-10, Iterations: 60}}, &optimize.NelderMead{})
		if err == nil && !result.Status.Early() && numberPointer(result.F) != nil && (best == nil || result.F < best.F) {
			best = result
		}
	}
	if best == nil {
		out.Status = "fit did not converge"
		return out
	}
	df, loc, scale := math.Exp(best.X[0]), best.X[1], math.Exp(best.X[2])
	out.DF, out.Location, out.Scale = numberPointer(df), numberPointer(loc), numberPointer(scale)
	out.Status = "converged"
	if df > 9990 || df < .0501 || scale < .0501 {
		out.Status = "converged at parameter bound; interpret with caution"
	}
	low, high := values[0].X, values[0].X
	for _, p := range values {
		low = math.Min(low, p.X)
		high = math.Max(high, p.X)
	}
	dist := distuv.StudentsT{Mu: loc, Sigma: scale, Nu: df}
	for i := 0; i <= 240; i++ {
		x := low + (high-low)*float64(i)/240
		y := dist.Prob(x)
		if numberPointer(y) != nil {
			out.Density = append(out.Density, analysisPoint{x, y})
		}
	}
	return out
}
