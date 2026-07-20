package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/storage"
)

var (
	ErrOverviewTargetNotConfigured = errors.New("尚未配置 Sub2API 同步目标")
	ErrOverviewMultipleTargets     = errors.New("检测到多个 Sub2API 同步目标，无法确定聚合目标")
)

type OverviewTargetDTO struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
}

type OverviewSummaryDTO struct {
	TotalAccounts       int `json:"total_accounts"`
	ActiveAccounts      int `json:"active_accounts"`
	SchedulableAccounts int `json:"schedulable_accounts"`
	ManagedAccounts     int `json:"managed_accounts"`
	UnmanagedAccounts   int `json:"unmanaged_accounts"`
}

type OverviewAccountGroupDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
}

type OverviewAccountDTO struct {
	ID                    int64                     `json:"id"`
	Name                  string                    `json:"name"`
	Platform              string                    `json:"platform"`
	Type                  string                    `json:"type"`
	Status                string                    `json:"status"`
	Schedulable           bool                      `json:"schedulable"`
	Concurrency           int                       `json:"concurrency"`
	Priority              int                       `json:"priority"`
	RateMultiplier        float64                   `json:"rate_multiplier"`
	LoadFactor            float64                   `json:"load_factor"`
	ProxyID               *int64                    `json:"proxy_id,omitempty"`
	ProxyName             string                    `json:"proxy_name,omitempty"`
	Groups                []OverviewAccountGroupDTO `json:"groups"`
	Managed               bool                      `json:"managed"`
	ManagedSyncGroupNames []string                  `json:"managed_sync_group_names"`
}

type OverviewGroupDTO struct {
	ID                      int64   `json:"id"`
	Name                    string  `json:"name"`
	Platform                string  `json:"platform,omitempty"`
	Ratio                   float64 `json:"ratio"`
	Status                  string  `json:"status"`
	Sort                    int     `json:"sort"`
	AccountCount            int     `json:"account_count"`
	ActiveAccountCount      int     `json:"active_account_count"`
	SchedulableAccountCount int     `json:"schedulable_account_count"`
}

type OverviewDTO struct {
	Target   OverviewTargetDTO    `json:"target"`
	Summary  OverviewSummaryDTO   `json:"summary"`
	Groups   []OverviewGroupDTO   `json:"groups"`
	Accounts []OverviewAccountDTO `json:"accounts"`
}

type SchedulableUpdateDTO struct {
	AccountID   int64 `json:"account_id"`
	Schedulable bool  `json:"schedulable"`
}

func (s *Service) GetOverview(ctx context.Context) (*OverviewDTO, error) {
	target, adminTarget, err := s.overviewTarget()
	if err != nil {
		return nil, err
	}
	client := sub2api.NewAdminClient()
	accounts, err := client.ListAllAccounts(ctx, adminTarget)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 账号失败: %w", err)
	}
	groups, err := client.ListGroups(ctx, adminTarget, true)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 分组失败: %w", err)
	}
	proxies, err := client.ListProxies(ctx, adminTarget)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 代理失败: %w", err)
	}
	managedNames, err := s.overviewManagedNames(target.ID)
	if err != nil {
		return nil, err
	}

	groupByID := make(map[int64]sub2api.AdminGroup, len(groups))
	groupStats := make(map[int64]*OverviewGroupDTO, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
		groupStats[group.ID] = &OverviewGroupDTO{
			ID: group.ID, Name: group.Name, Platform: group.Platform,
			Ratio: group.Ratio, Status: normalizeOverviewStatus(group.Status), Sort: group.Sort,
		}
	}
	proxyNames := make(map[int64]string, len(proxies))
	for _, proxy := range proxies {
		proxyNames[proxy.ID] = proxy.Name
	}

	out := &OverviewDTO{
		Target:   OverviewTargetDTO{ID: target.ID, Name: target.Name, BaseURL: target.BaseURL, Enabled: target.Enabled},
		Groups:   make([]OverviewGroupDTO, 0, len(groups)+1),
		Accounts: make([]OverviewAccountDTO, 0, len(accounts)),
	}
	var ungrouped *OverviewGroupDTO
	for _, account := range accounts {
		item := OverviewAccountDTO{
			ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type,
			Status: normalizeOverviewStatus(account.Status), Schedulable: account.Schedulable,
			Concurrency: account.Concurrency, Priority: account.Priority,
			RateMultiplier: account.RateMultiplier, LoadFactor: account.LoadFactor,
			ProxyID: account.ProxyID, Groups: make([]OverviewAccountGroupDTO, 0, len(account.GroupIDs)),
			ManagedSyncGroupNames: append([]string(nil), managedNames[account.ID]...),
		}
		item.Managed = len(item.ManagedSyncGroupNames) > 0
		if account.ProxyID != nil {
			item.ProxyName = proxyNames[*account.ProxyID]
			if item.ProxyName == "" {
				item.ProxyName = fmt.Sprintf("代理 #%d", *account.ProxyID)
			}
		}
		for _, groupID := range account.GroupIDs {
			group, ok := groupByID[groupID]
			if !ok {
				item.Groups = append(item.Groups, OverviewAccountGroupDTO{ID: groupID, Name: fmt.Sprintf("分组 #%d", groupID)})
				continue
			}
			item.Groups = append(item.Groups, OverviewAccountGroupDTO{ID: group.ID, Name: group.Name, Platform: group.Platform})
			incrementOverviewGroup(groupStats[groupID], item)
		}
		if len(account.GroupIDs) == 0 {
			if ungrouped == nil {
				ungrouped = &OverviewGroupDTO{ID: 0, Name: "未分组", Status: "active", Sort: int(^uint(0) >> 1)}
			}
			incrementOverviewGroup(ungrouped, item)
		}
		out.Accounts = append(out.Accounts, item)
		out.Summary.TotalAccounts++
		if isOverviewActive(item.Status) {
			out.Summary.ActiveAccounts++
		}
		if item.Schedulable {
			out.Summary.SchedulableAccounts++
		}
		if item.Managed {
			out.Summary.ManagedAccounts++
		} else {
			out.Summary.UnmanagedAccounts++
		}
	}
	for _, group := range groupStats {
		out.Groups = append(out.Groups, *group)
	}
	if ungrouped != nil {
		out.Groups = append(out.Groups, *ungrouped)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		if out.Groups[i].Sort != out.Groups[j].Sort {
			return out.Groups[i].Sort < out.Groups[j].Sort
		}
		return strings.ToLower(out.Groups[i].Name) < strings.ToLower(out.Groups[j].Name)
	})
	sort.Slice(out.Accounts, func(i, j int) bool {
		return strings.ToLower(out.Accounts[i].Name) < strings.ToLower(out.Accounts[j].Name)
	})
	return out, nil
}

func (s *Service) SetOverviewAccountSchedulable(ctx context.Context, accountID int64, schedulable bool) (*SchedulableUpdateDTO, error) {
	if accountID <= 0 {
		return nil, errors.New("账号 ID 必须大于 0")
	}
	_, adminTarget, err := s.overviewTarget()
	if err != nil {
		return nil, err
	}
	account, err := sub2api.NewAdminClient().SetAccountSchedulable(ctx, adminTarget, accountID, schedulable)
	if err != nil {
		return nil, fmt.Errorf("更新 Sub2API 账号调度失败: %w", err)
	}
	return &SchedulableUpdateDTO{AccountID: account.ID, Schedulable: account.Schedulable}, nil
}

func (s *Service) overviewTarget() (*storage.UpstreamSyncTarget, sub2api.AdminTarget, error) {
	targets, err := s.targets.List()
	if err != nil {
		return nil, sub2api.AdminTarget{}, err
	}
	if len(targets) == 0 {
		return nil, sub2api.AdminTarget{}, ErrOverviewTargetNotConfigured
	}
	if len(targets) != 1 {
		return nil, sub2api.AdminTarget{}, ErrOverviewMultipleTargets
	}
	plain, err := s.cipher.Decrypt(targets[0].AdminAPIKeyCipher)
	if err != nil {
		return nil, sub2api.AdminTarget{}, fmt.Errorf("解密 Sub2API Admin Key 失败: %w", err)
	}
	return &targets[0], sub2api.AdminTarget{BaseURL: targets[0].BaseURL, APIKey: plain}, nil
}

func (s *Service) overviewManagedNames(targetID uint) (map[int64][]string, error) {
	syncGroups, err := s.syncGroups.List()
	if err != nil {
		return nil, err
	}
	managed, err := s.managedAccounts.List()
	if err != nil {
		return nil, err
	}
	groupNames := make(map[uint]string)
	for _, group := range syncGroups {
		if group.TargetID != targetID {
			continue
		}
		name := strings.TrimSpace(group.DisplayName)
		if name == "" {
			name = group.Name
		}
		groupNames[group.ID] = name
	}
	out := make(map[int64][]string)
	seen := make(map[int64]map[string]struct{})
	for _, item := range managed {
		name, ok := groupNames[item.SyncGroupID]
		if !ok {
			continue
		}
		if seen[item.TargetAccountID] == nil {
			seen[item.TargetAccountID] = make(map[string]struct{})
		}
		if _, exists := seen[item.TargetAccountID][name]; exists {
			continue
		}
		seen[item.TargetAccountID][name] = struct{}{}
		out[item.TargetAccountID] = append(out[item.TargetAccountID], name)
	}
	for id := range out {
		sort.Strings(out[id])
	}
	return out, nil
}

func incrementOverviewGroup(group *OverviewGroupDTO, account OverviewAccountDTO) {
	group.AccountCount++
	if isOverviewActive(account.Status) {
		group.ActiveAccountCount++
	}
	if account.Schedulable {
		group.SchedulableAccountCount++
	}
}

func normalizeOverviewStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "active"
	}
	return status
}

func isOverviewActive(status string) bool {
	return normalizeOverviewStatus(status) == "active"
}
