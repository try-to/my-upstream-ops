package syncer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/storage"
)

type ratePolicyTargetKey struct {
	TargetID  uint
	AccountID int64
}

type ratePolicyDecision struct {
	Target      *storage.UpstreamSyncTarget
	SyncGroupID uint
	AccountID   int64
	Enabled     bool
	Reasons     []string
}

func (s *Service) ReconcileChannelRatePolicies(ctx context.Context, channelID uint) error {
	if s.ratePolicies == nil {
		return nil
	}
	policies, err := s.listRatePolicies(channelID)
	if err != nil || len(policies) == 0 {
		return err
	}
	snapshotsByChannel, err := s.rateSnapshotsByChannel(policies)
	if err != nil {
		return err
	}
	managed, err := s.managedAccounts.List()
	if err != nil {
		return err
	}
	managedBySyncAccount := make(map[uint]storage.UpstreamSyncManagedAccount, len(managed))
	for _, item := range managed {
		managedBySyncAccount[item.SyncAccountID] = item
	}
	syncGroups, err := s.syncGroups.List()
	if err != nil {
		return err
	}
	decisions := make(map[ratePolicyTargetKey]*ratePolicyDecision)
	var collected []error
	for i := range syncGroups {
		syncGroup := &syncGroups[i]
		if !syncGroup.Enabled {
			continue
		}
		target, err := s.targets.FindByID(syncGroup.TargetID)
		if err != nil {
			collected = append(collected, err)
			continue
		}
		if !target.Enabled {
			continue
		}
		accounts, err := s.syncAccounts.ListBySyncGroupID(syncGroup.ID)
		if err != nil {
			collected = append(collected, err)
			continue
		}
		for accountIndex := range accounts {
			account := &accounts[accountIndex]
			if !account.Enabled || (channelID != 0 && account.SourceChannelID != channelID) {
				continue
			}
			policy := matchRatePolicy(policies, account)
			if policy == nil {
				continue
			}
			snapshot := matchRateSnapshot(snapshotsByChannel[account.SourceChannelID], policy)
			if snapshot == nil || !validRatePolicyValues(snapshot.Ratio, policy.MaxRatio, policy.CalculationRatio) {
				continue
			}
			mapped, ok := managedBySyncAccount[account.ID]
			if !ok || mapped.TargetAccountID <= 0 {
				continue
			}
			calculated := snapshot.Ratio * policy.CalculationRatio
			enabled := calculated <= policy.MaxRatio
			key := ratePolicyTargetKey{TargetID: target.ID, AccountID: mapped.TargetAccountID}
			decision := decisions[key]
			if decision == nil {
				copyTarget := *target
				decision = &ratePolicyDecision{Target: &copyTarget, SyncGroupID: syncGroup.ID, AccountID: mapped.TargetAccountID, Enabled: true}
				decisions[key] = decision
			}
			decision.Enabled = decision.Enabled && enabled
			decision.Reasons = append(decision.Reasons, fmt.Sprintf(
				"%s：%s * %s = %s，最大 %s",
				policy.GroupName, formatNumber(snapshot.Ratio), formatNumber(policy.CalculationRatio),
				formatNumber(calculated), formatNumber(policy.MaxRatio),
			))
		}
	}
	if err := s.applyRatePolicyDecisions(ctx, decisions); err != nil {
		collected = append(collected, err)
	}
	return errors.Join(collected...)
}

func (s *Service) ReconcileSyncGroupRatePolicies(ctx context.Context, syncGroupID uint) error {
	accounts, err := s.syncAccounts.ListBySyncGroupID(syncGroupID)
	if err != nil {
		return err
	}
	seen := make(map[uint]struct{})
	var collected []error
	for _, account := range accounts {
		if _, ok := seen[account.SourceChannelID]; ok {
			continue
		}
		seen[account.SourceChannelID] = struct{}{}
		if err := s.ReconcileChannelRatePolicies(ctx, account.SourceChannelID); err != nil {
			collected = append(collected, err)
		}
	}
	return errors.Join(collected...)
}

func (s *Service) listRatePolicies(channelID uint) ([]storage.RateGroupPolicy, error) {
	if channelID != 0 {
		return s.ratePolicies.ListByChannel(channelID)
	}
	return s.ratePolicies.List()
}

func (s *Service) rateSnapshotsByChannel(policies []storage.RateGroupPolicy) (map[uint][]storage.RateSnapshot, error) {
	out := make(map[uint][]storage.RateSnapshot)
	for _, policy := range policies {
		if _, ok := out[policy.ChannelID]; ok {
			continue
		}
		list, err := s.rates.ListByChannel(policy.ChannelID)
		if err != nil {
			return nil, err
		}
		out[policy.ChannelID] = list
	}
	return out, nil
}

func matchRatePolicy(policies []storage.RateGroupPolicy, account *storage.UpstreamSyncAccount) *storage.RateGroupPolicy {
	if account == nil {
		return nil
	}
	if account.SourceGroupID != nil {
		for i := range policies {
			if policies[i].ChannelID == account.SourceChannelID && policies[i].RemoteGroupID != nil && *policies[i].RemoteGroupID == *account.SourceGroupID {
				return &policies[i]
			}
		}
	}
	name := strings.TrimSpace(account.SourceGroupName)
	for i := range policies {
		if policies[i].ChannelID == account.SourceChannelID && name != "" && strings.EqualFold(strings.TrimSpace(policies[i].GroupName), name) {
			return &policies[i]
		}
	}
	return nil
}

func matchRateSnapshot(snapshots []storage.RateSnapshot, policy *storage.RateGroupPolicy) *storage.RateSnapshot {
	if policy == nil {
		return nil
	}
	if policy.RemoteGroupID != nil {
		for i := range snapshots {
			if snapshots[i].RemoteGroupID != nil && *snapshots[i].RemoteGroupID == *policy.RemoteGroupID {
				return &snapshots[i]
			}
		}
	}
	for i := range snapshots {
		if strings.EqualFold(strings.TrimSpace(snapshots[i].ModelName), strings.TrimSpace(policy.GroupName)) {
			return &snapshots[i]
		}
	}
	return nil
}

func validRatePolicyValues(rawRatio, maxRatio, calculationRatio float64) bool {
	for _, value := range []float64{rawRatio, maxRatio, calculationRatio} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return rawRatio >= 0 && maxRatio >= 0 && calculationRatio > 0
}

func (s *Service) applyRatePolicyDecisions(ctx context.Context, decisions map[ratePolicyTargetKey]*ratePolicyDecision) error {
	byTarget := make(map[uint][]*ratePolicyDecision)
	for _, decision := range decisions {
		byTarget[decision.Target.ID] = append(byTarget[decision.Target.ID], decision)
	}
	client := sub2api.NewAdminClient()
	var collected []error
	for _, targetDecisions := range byTarget {
		target := targetDecisions[0].Target
		plain, err := s.cipher.Decrypt(target.AdminAPIKeyCipher)
		if err != nil {
			collected = append(collected, fmt.Errorf("解密目标 %s Admin Key 失败: %w", target.Name, err))
			continue
		}
		adminTarget := sub2api.AdminTarget{BaseURL: target.BaseURL, APIKey: plain}
		accounts, err := client.ListAllAccounts(ctx, adminTarget)
		if err != nil {
			collected = append(collected, fmt.Errorf("读取目标 %s 账号失败: %w", target.Name, err))
			continue
		}
		currentByID := make(map[int64]sub2api.AdminAccount, len(accounts))
		for _, account := range accounts {
			currentByID[account.ID] = account
		}
		for _, decision := range targetDecisions {
			current, ok := currentByID[decision.AccountID]
			if !ok || current.Schedulable == decision.Enabled {
				continue
			}
			if _, err := client.SetAccountSchedulable(ctx, adminTarget, decision.AccountID, decision.Enabled); err != nil {
				collected = append(collected, fmt.Errorf("更新目标 %s 账号 #%d 调度失败: %w", target.Name, decision.AccountID, err))
				continue
			}
			state := "启用"
			if !decision.Enabled {
				state = "禁用"
			}
			message := fmt.Sprintf("渠道分组倍率策略%s账号 #%d 调度：%s", state, decision.AccountID, strings.Join(decision.Reasons, "；"))
			if _, err := s.appendLog(decision.SyncGroupID, target.ID, "rate_policy", true, message); err != nil {
				collected = append(collected, err)
			}
		}
	}
	return errors.Join(collected...)
}
