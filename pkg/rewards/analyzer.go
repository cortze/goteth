package rewards

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/cortze/eth2-state-analyzer/pkg/clientapi"
	"github.com/cortze/eth2-state-analyzer/pkg/db/postgresql"
	"github.com/cortze/eth2-state-analyzer/pkg/reward_metrics"
	"github.com/cortze/eth2-state-analyzer/pkg/reward_metrics/fork_state"
	"github.com/cortze/eth2-state-analyzer/pkg/utils"
)

var (
	modName = "Analyzer"
	log     = logrus.WithField(
		"module", modName,
	)
	maxWorkers         = 50
	minReqTime         = 10 * time.Second
	MAX_VAL_BATCH_SIZE = 20000
	VAL_LEN            = 400000
	SLOT_SECONDS       = 12
	EPOCH_SLOTS        = 32
)

type StateAnalyzer struct {
	ctx                context.Context
	cancel             context.CancelFunc
	InitSlot           uint64
	FinalSlot          uint64
	ValidatorIndexes   []uint64
	SlotRanges         []uint64
	MonitorSlotProcess map[uint64]uint64
	EpochTaskChan      chan *EpochTask
	ValTaskChan        chan *ValTask
	validatorWorkerNum int

	cli      *clientapi.APIClient
	dbClient *postgresql.PostgresDBService

	// Control Variables
	finishDownload bool
	routineClosed  chan struct{}

	initTime time.Time
}

func NewStateAnalyzer(
	pCtx context.Context,
	httpCli *clientapi.APIClient,
	initSlot uint64,
	finalSlot uint64,
	valIdxs []uint64,
	idbUrl string,
	workerNum int,
	dbWorkerNum int) (*StateAnalyzer, error) {
	log.Infof("generating new State Analzyer from slots %d:%d, for validators %v", initSlot, finalSlot, valIdxs)
	// gen new ctx from parent
	ctx, cancel := context.WithCancel(pCtx)

	// Check if the range of slots is valid
	if !utils.IsValidRangeuint64(initSlot, finalSlot) {
		return nil, errors.New("provided slot range isn't valid")
	}

	valLength := len(valIdxs)
	// check if valIdx where given
	if len(valIdxs) < 1 {
		log.Infof("No validator indexes provided: running all validators")
		valLength = VAL_LEN // estimation to declare channels
	}

	// calculate the list of slots that we will analyze
	slotRanges := make([]uint64, 0)
	epochRange := uint64(0)

	// minimum slot is 31
	// force to be in the previous epoch than select by user
	initEpoch := uint64(initSlot) / 32
	finalEpoch := uint64(finalSlot / 32)

	initSlot = (initEpoch+1)*fork_state.SLOTS_PER_EPOCH - 1   // take last slot of init Epoch
	finalSlot = (finalEpoch+1)*fork_state.SLOTS_PER_EPOCH - 1 // take last slot of final Epoch

	// start two epochs before and end two epochs after
	for i := initSlot - (fork_state.SLOTS_PER_EPOCH * 2); i <= (finalSlot + fork_state.SLOTS_PER_EPOCH*2); i += utils.SlotBase {
		slotRanges = append(slotRanges, i)
		epochRange++
	}
	log.Debug("slotRanges are:", slotRanges)

	i_dbClient, err := postgresql.ConnectToDB(ctx, idbUrl, valLength*maxWorkers, dbWorkerNum)
	if err != nil {
		return nil, errors.Wrap(err, "unable to generate DB Client.")
	}

	return &StateAnalyzer{
		ctx:                ctx,
		cancel:             cancel,
		InitSlot:           initSlot,
		FinalSlot:          finalSlot,
		ValidatorIndexes:   valIdxs,
		SlotRanges:         slotRanges,
		EpochTaskChan:      make(chan *EpochTask, 10),
		ValTaskChan:        make(chan *ValTask, valLength*maxWorkers),
		MonitorSlotProcess: make(map[uint64]uint64),
		cli:                httpCli,
		dbClient:           i_dbClient,
		validatorWorkerNum: workerNum,
		routineClosed:      make(chan struct{}),
	}, nil
}

func (s *StateAnalyzer) Run() {
	defer s.cancel()
	// Get init time
	s.initTime = time.Now()

	log.Info("State Analyzer initialized at ", s.initTime)

	// State requester
	var wgDownload sync.WaitGroup
	downloadFinishedFlag := false

	// Rewards per process
	var wgProcess sync.WaitGroup
	processFinishedFlag := false

	// Workers to process each validator rewards
	var wgWorkers sync.WaitGroup

	totalTime := int64(0)
	start := time.Now()

	// State requester + Task generator
	wgDownload.Add(1)
	go s.runDownloadStates(&wgDownload)

	// State requester in finalized slots, not used for now
	// wgDownload.Add(1)
	// go s.runDownloadStatesFinalized(&wgDownload)

	wgProcess.Add(1)
	go s.runProcessState(&wgProcess, &downloadFinishedFlag)

	for i := 0; i < s.validatorWorkerNum; i++ {
		// state workers, receiving State and valIdx to measure performance
		wlog := logrus.WithField(
			"worker", i,
		)

		wlog.Tracef("Launching Task Worker")
		wgWorkers.Add(1)
		go s.runWorker(wlog, &wgWorkers, &processFinishedFlag)
	}

	wgDownload.Wait()
	downloadFinishedFlag = true
	log.Info("Beacon State Downloads finished")

	wgProcess.Wait()
	processFinishedFlag = true
	log.Info("Beacon State Processing finished")

	wgWorkers.Wait()
	log.Info("All validator workers finished")
	s.dbClient.DoneTasks()
	<-s.dbClient.FinishSignalChan

	close(s.ValTaskChan)
	totalTime += int64(time.Since(start).Seconds())
	analysisDuration := time.Since(s.initTime).Seconds()
	log.Info("State Analyzer finished in ", analysisDuration)
	if s.finishDownload {
		s.routineClosed <- struct{}{}
	}
}

func (s *StateAnalyzer) Close() {
	log.Info("Sudden closed detected, closing StateAnalyzer")
	s.finishDownload = true
	<-s.routineClosed
	s.cancel()
}

//
type EpochTask struct {
	ValIdxs     []uint64
	NextState   fork_state.ForkStateContentBase
	State       fork_state.ForkStateContentBase
	PrevState   fork_state.ForkStateContentBase
	OnlyPrevAtt bool
}

type ValTask struct {
	ValIdxs         []uint64
	StateMetricsObj reward_metrics.StateMetrics
	OnlyPrevAtt     bool
}

type MonitorTasks struct {
	ValIdxs []uint64
	Slot    uint64
}