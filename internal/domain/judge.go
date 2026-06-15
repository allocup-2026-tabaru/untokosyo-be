package domain

import (
	"math"
	"math/rand"
)

type JudgeInput struct {
	TotalPullAccumulation float64
	ActivePlayerCount     int
	PullingPlayerCount    int
	ElapsedSeconds        float64
}

type ExtractionJudge interface {
	Judge(input JudgeInput) (probability float64, extracted bool)
}

type SigmoidJudge struct {
	Midpoint  float64
	Steepness float64
}

func (j *SigmoidJudge) Judge(input JudgeInput) (probability float64, extracted bool) {
	x := input.TotalPullAccumulation
	probability = 1.0 / (1.0 + math.Exp(-j.Steepness*(x-j.Midpoint)))
	if input.PullingPlayerCount == 0 {
		return probability, false
	}
	return probability, rand.Float64() < probability
}

type JudgeType string

const (
	JudgeTypeSigmoid     JudgeType = "sigmoid"
	JudgeTypeExponential JudgeType = "exponential"
	JudgeTypeStep        JudgeType = "step"
)

type JudgeConfig struct {
	Type   JudgeType
	Params map[string]float64
}

func NewExtractionJudge(cfg JudgeConfig) ExtractionJudge {
	midpoint := cfg.Params["midpoint"]
	if midpoint == 0 {
		midpoint = 100.0
	}
	steepness := cfg.Params["steepness"]
	if steepness == 0 {
		steepness = 0.05
	}
	return &SigmoidJudge{Midpoint: midpoint, Steepness: steepness}
}
