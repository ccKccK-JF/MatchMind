package application

import (
	"context"
	"runtime"
	"sync"

	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

const MaxBatchSimulationCases = 10000

type BatchReport struct {
	Results              []simulationdomain.Result
	SimulationCount      int
	TeamAWinRate         float64
	AverageDuration      float64
	AverageActualQuality float64
	OneSidedRate         float64
	AFKRate              float64
	SurrenderRate        float64
}

func (s *Service) SimulateBatch(
	ctx context.Context,
	inputs []simulationdomain.Input,
	concurrency int,
) (BatchReport, error) {
	if len(inputs) == 0 || len(inputs) > MaxBatchSimulationCases {
		return BatchReport{}, simulationdomain.ErrInvalidSimulation
	}
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	if concurrency > 64 {
		concurrency = 64
	}
	if concurrency > len(inputs) {
		concurrency = len(inputs)
	}

	workerContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]simulationdomain.Result, len(inputs))
	jobs := make(chan int)
	var waitGroup sync.WaitGroup
	var errorOnce sync.Once
	var simulationErr error
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-workerContext.Done():
					return
				case index, open := <-jobs:
					if !open {
						return
					}
					result, err := s.simulator.Simulate(inputs[index])
					if err != nil {
						errorOnce.Do(func() {
							simulationErr = err
							cancel()
						})
						return
					}
					results[index] = result
				}
			}
		}()
	}
	feedComplete := true
	for index := range inputs {
		select {
		case jobs <- index:
		case <-workerContext.Done():
			feedComplete = false
		}
		if !feedComplete {
			break
		}
	}
	close(jobs)
	waitGroup.Wait()
	if simulationErr != nil {
		return BatchReport{}, simulationErr
	}
	if err := ctx.Err(); err != nil {
		return BatchReport{}, err
	}
	return summarizeBatch(results), nil
}

func summarizeBatch(results []simulationdomain.Result) BatchReport {
	report := BatchReport{Results: append([]simulationdomain.Result(nil), results...), SimulationCount: len(results)}
	if len(results) == 0 {
		return report
	}
	for _, result := range results {
		if result.WinningTeam == simulationdomain.WinningTeamA {
			report.TeamAWinRate++
		}
		report.AverageDuration += float64(result.DurationSeconds)
		report.AverageActualQuality += result.ActualQualityScore
		if result.OneSided {
			report.OneSidedRate++
		}
		if result.HasAFK {
			report.AFKRate++
		}
		if result.Surrendered {
			report.SurrenderRate++
		}
	}
	count := float64(len(results))
	report.TeamAWinRate /= count
	report.AverageDuration /= count
	report.AverageActualQuality /= count
	report.OneSidedRate /= count
	report.AFKRate /= count
	report.SurrenderRate /= count
	return report
}
