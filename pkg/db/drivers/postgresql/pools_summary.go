package postgresql

import (
	"fmt"

	"github.com/cortze/eth-cl-state-analyzer/pkg/db/model"
	"github.com/pkg/errors"
)

// Postgres intregration variables
var (
	CreatePoolSummaryTable = `
	CREATE TABLE IF NOT EXISTS t_pool_summary(
		f_pool_name TEXT,
		f_epoch INT,
		f_avg_reward INT,
		f_avg_max_reward INT,
		f_avg_max_att_reward INT,
		f_avg_max_sync_reward INT,
		f_avg_base_reward INT,
		f_sum_missing_source INT,
		f_sum_missing_target INT, 
		f_sum_missing_head INT,
		f_num_active_vals INT,
		f_sync_vals INT,
		CONSTRAINT PK_EpochPool PRIMARY KEY (f_pool_name,f_epoch));`

	UpsertPoolSummary = `
	INSERT INTO t_pool_summary (
		f_pool_name,
		f_epoch,
		f_avg_reward,
		f_avg_max_reward,
		f_avg_max_att_reward,
		f_avg_max_sync_reward,
		f_avg_base_reward,
		f_sum_missing_source,
		f_sum_missing_target,
		f_sum_missing_head,
		f_num_active_vals,
		f_sync_vals)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	ON CONFLICT ON CONSTRAINT PK_EpochPool
		DO 
			UPDATE SET 
			f_avg_reward = excluded.f_avg_reward,
			f_avg_max_reward = excluded.f_avg_max_reward,
			f_avg_max_att_reward = excluded.f_avg_max_att_reward,
			f_avg_max_sync_reward = excluded.f_avg_max_sync_reward,
			f_avg_base_reward = excluded.f_avg_base_reward,
			f_sum_missing_source = excluded.f_sum_missing_source,
			f_sum_missing_target  = excluded.f_sum_missing_target,  
			f_sum_missing_head = excluded.f_sum_missing_head,
			f_num_active_vals = excluded.f_num_active_vals,
			f_sync_vals = excluded.f_sync_vals;
	`
)

func insertPool(inputPool model.PoolSummary) (string, []interface{}) {
	resultArgs := make([]interface{}, 0)

	if len(inputPool.ValidatorList) > 0 {
		reward := float32(0)
		maxReward := float32(0)
		baseReward := float32(0)
		attMaxReward := float32(0)
		syncMaxReward := float32(0)
		missingSource := 0
		missingTarget := 0
		missingHead := 0

		// numSyncVals := 0
		// numProposers := 0
		// numActiveVals := 0

		// calculate averages
		for _, item := range inputPool.ValidatorList {
			reward += float32(item.Reward)
			reward += float32(item.MaxReward)
			reward += float32(item.BaseReward)
			reward += float32(item.AttestationReward)
			reward += float32(item.SyncCommitteeReward)

			if item.MissingSource {
				missingSource += 1
			}
			if item.MissingTarget {
				missingTarget += 1
			}
			if item.MissingHead {
				missingHead += 1
			}
		}

		resultArgs = append(resultArgs, inputPool.PoolName)
		resultArgs = append(resultArgs, inputPool.Epoch)
		resultArgs = append(resultArgs, reward)
		resultArgs = append(resultArgs, maxReward)
		resultArgs = append(resultArgs, attMaxReward)
		resultArgs = append(resultArgs, syncMaxReward)
		resultArgs = append(resultArgs, baseReward)
	}

	return UpsertPoolSummary, resultArgs
}

func PoolOperation(inputPool model.PoolSummary, op string) (string, []interface{}, error) {

	if op == model.INSERT_OP {
		q, args := insertPool(inputPool)
		return q, args, nil
	}

	return "", nil, fmt.Errorf("pool operation not permitted: %s", op)
}

func (p *PostgresDBService) createPoolsTable() error {
	// create the tables
	_, err := p.psqlPool.Exec(p.ctx, CreatePoolSummaryTable)
	if err != nil {
		return errors.Wrap(err, "error creating pools table")
	}
	return nil
}