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
	activeCount := float64(input.ActivePlayerCount)
	if activeCount < 1 {
		activeCount = 1
	}

	pullingCount := float64(input.PullingPlayerCount)
	if pullingCount < 0 {
		pullingCount = 0
	}

	elapsedSeconds := input.ElapsedSeconds
	if elapsedSeconds < 0 {
		elapsedSeconds = 0
	}

	// 累積は主材料。人数が増えたときに極端に跳ねないよう、少しだけ逓減させる。
	normalizedAccumulation := input.TotalPullAccumulation / math.Sqrt(activeCount)

	// 現在の圧力は「引いている人数」の影響を圧縮しつつ、確実に押し上げる。
	instantPressure := math.Sqrt(pullingCount) * instantPressureWeight * 12.0

	// 長引くほど少しずつ抜けやすくして、膠着を減らす。
	timePressure := math.Log1p(elapsedSeconds) * 4.0

	x := normalizedAccumulation + instantPressure + timePressure
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
