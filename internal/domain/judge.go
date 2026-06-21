package domain

import (
	"math"
	"math/rand"
)

const instantPressureWeight = 0.25

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
	// 累積を主軸にしつつ、今この瞬間の引っ張り人数を少しだけ上乗せする。
	// これで「ずっと引いている重み」は保ちつつ、現在の圧力も反映する。
	x := input.TotalPullAccumulation + float64(input.PullingPlayerCount)*instantPressureWeight
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
