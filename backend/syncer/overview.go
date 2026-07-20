package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bejix/upstream-ops/backend/connector/sub2api"
	"github.com/bejix/upstream-ops/backend/storage"
)

var (
	ErrOverviewTargetNotConfigured = errors.New("尚未配置 Sub2API 同步目标")
	ErrOverviewMultipleTargets     = errors.New("检测到多个 Sub2API 同步目标，无法确定聚合目标")
	ErrInvalidOverviewRouting      = errors.New("智能调度路由参数无效")
)

const (
	smartDispatchFallbackKey        = "__fallback__"
	smartDispatchPrimaryWeightsKey  = "__weights_primary__"
	smartDispatchFallbackWeightsKey = "__weights_fallback__"
	smartDispatchDefaultWeight      = 100

	virtualOAuthPlusID = int64(-10001)
	virtualOAuthProID  = int64(-10002)
	virtualOAuthK12ID  = int64(-10003)
	virtualOAuthTeamID = int64(-10004)
	virtualOAuthFreeID = int64(-10005)
)

var virtualOAuthPoolNames = map[int64]string{
	virtualOAuthPlusID: "OpenAI OAuth Plus",
	virtualOAuthProID:  "OpenAI OAuth Pro",
	virtualOAuthK12ID:  "OpenAI OAuth K12",
	virtualOAuthTeamID: "OpenAI OAuth Team",
	virtualOAuthFreeID: "OpenAI OAuth Free",
}

type OverviewTargetDTO struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Enabled bool   `json:"enabled"`
}

type OverviewSummaryDTO struct {
	TotalGroups          int `json:"total_groups"`
	SmartDispatchGroups  int `json:"smart_dispatch_groups"`
	RealUpstreamAccounts int `json:"real_upstream_accounts"`
	VirtualPools         int `json:"virtual_pools"`
	VirtualPoolMembers   int `json:"virtual_pool_members"`
}

type OverviewPoolEntryDTO struct {
	ID                    int64    `json:"id"`
	Name                  string   `json:"name"`
	Kind                  string   `json:"kind"`
	Weight                int      `json:"weight"`
	Available             bool     `json:"available"`
	MemberCount           int      `json:"member_count,omitempty"`
	Platform              string   `json:"platform,omitempty"`
	Type                  string   `json:"type,omitempty"`
	Status                string   `json:"status,omitempty"`
	Schedulable           bool     `json:"schedulable,omitempty"`
	Concurrency           int      `json:"concurrency,omitempty"`
	Priority              int      `json:"priority,omitempty"`
	RateMultiplier        float64  `json:"rate_multiplier,omitempty"`
	ProxyName             string   `json:"proxy_name,omitempty"`
	Managed               bool     `json:"managed,omitempty"`
	ManagedSyncGroupNames []string `json:"managed_sync_group_names,omitempty"`
}

type OverviewGroupDTO struct {
	ID                   int64                  `json:"id"`
	Name                 string                 `json:"name"`
	Platform             string                 `json:"platform,omitempty"`
	Ratio                float64                `json:"ratio"`
	Status               string                 `json:"status"`
	Sort                 int                    `json:"sort"`
	SmartDispatchEnabled bool                   `json:"smart_dispatch_enabled"`
	PrimaryPool          []OverviewPoolEntryDTO `json:"primary_pool"`
	FallbackPool         []OverviewPoolEntryDTO `json:"fallback_pool"`
}

type OverviewDTO struct {
	Target  OverviewTargetDTO  `json:"target"`
	Summary OverviewSummaryDTO `json:"summary"`
	Groups  []OverviewGroupDTO `json:"groups"`
}

type SchedulableUpdateDTO struct {
	AccountID   int64 `json:"account_id"`
	Schedulable bool  `json:"schedulable"`
}

type SmartRoutingEntryInput struct {
	ID     int64 `json:"id"`
	Weight int   `json:"weight"`
}

type SmartRoutingUpdateInput struct {
	PrimaryPool  []SmartRoutingEntryInput `json:"primary_pool"`
	FallbackPool []SmartRoutingEntryInput `json:"fallback_pool"`
}

type SmartRoutingUpdateDTO struct {
	GroupID              int64                    `json:"group_id"`
	SmartDispatchEnabled bool                     `json:"smart_dispatch_enabled"`
	PrimaryPool          []SmartRoutingEntryInput `json:"primary_pool"`
	FallbackPool         []SmartRoutingEntryInput `json:"fallback_pool"`
}

func (s *Service) GetOverview(ctx context.Context) (*OverviewDTO, error) {
	target, adminTarget, err := s.overviewTarget()
	if err != nil {
		return nil, err
	}
	client := sub2api.NewAdminClient()
	groups, err := client.ListGroups(ctx, adminTarget, true)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 分组失败: %w", err)
	}
	accounts, err := client.ListAllAccounts(ctx, adminTarget)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 智能调度账号失败: %w", err)
	}
	proxies, err := client.ListProxies(ctx, adminTarget)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 代理失败: %w", err)
	}
	managedNames, err := s.overviewManagedNames(target.ID)
	if err != nil {
		return nil, err
	}

	accountByID := make(map[int64]sub2api.AdminAccount, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	proxyNames := make(map[int64]string, len(proxies))
	for _, proxy := range proxies {
		proxyNames[proxy.ID] = proxy.Name
	}
	virtualCounts := countVirtualOAuthPools(accounts, time.Now())

	out := &OverviewDTO{
		Target: OverviewTargetDTO{ID: target.ID, Name: target.Name, BaseURL: target.BaseURL, Enabled: target.Enabled},
		Groups: make([]OverviewGroupDTO, 0, len(groups)),
	}
	realIDs := make(map[int64]struct{})
	virtualIDs := make(map[int64]struct{})
	for _, group := range groups {
		item := OverviewGroupDTO{
			ID: group.ID, Name: group.Name, Platform: group.Platform,
			Ratio: group.Ratio, Status: normalizeOverviewStatus(group.Status), Sort: overviewGroupSort(group),
			SmartDispatchEnabled: group.ModelRoutingEnabled,
			PrimaryPool:          buildOverviewPool(group.ModelRouting["*"], group.ModelRouting[smartDispatchPrimaryWeightsKey], accountByID, proxyNames, managedNames, virtualCounts),
			FallbackPool:         buildOverviewPool(group.ModelRouting[smartDispatchFallbackKey], group.ModelRouting[smartDispatchFallbackWeightsKey], accountByID, proxyNames, managedNames, virtualCounts),
		}
		if len(item.PrimaryPool) == 0 && len(item.FallbackPool) == 0 {
			continue
		}
		if item.SmartDispatchEnabled {
			out.Summary.SmartDispatchGroups++
		}
		for _, entry := range append(append([]OverviewPoolEntryDTO(nil), item.PrimaryPool...), item.FallbackPool...) {
			if entry.Kind == "virtual" {
				virtualIDs[entry.ID] = struct{}{}
			} else if entry.ID > 0 {
				realIDs[entry.ID] = struct{}{}
			}
		}
		out.Groups = append(out.Groups, item)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		if out.Groups[i].Sort != out.Groups[j].Sort {
			return out.Groups[i].Sort < out.Groups[j].Sort
		}
		return strings.ToLower(out.Groups[i].Name) < strings.ToLower(out.Groups[j].Name)
	})
	out.Summary.TotalGroups = len(out.Groups)
	out.Summary.RealUpstreamAccounts = len(realIDs)
	out.Summary.VirtualPools = len(virtualIDs)
	for id := range virtualIDs {
		out.Summary.VirtualPoolMembers += virtualCounts[id]
	}
	return out, nil
}

func buildOverviewPool(
	ids []int64,
	weights []int64,
	accountByID map[int64]sub2api.AdminAccount,
	proxyNames map[int64]string,
	managedNames map[int64][]string,
	virtualCounts map[int64]int,
) []OverviewPoolEntryDTO {
	out := make([]OverviewPoolEntryDTO, 0, len(ids))
	for index, id := range ids {
		weight := smartDispatchDefaultWeight
		if index < len(weights) {
			weight = normalizeSmartDispatchWeight(weights[index])
		}
		if name, ok := virtualOAuthPoolNames[id]; ok {
			count := virtualCounts[id]
			out = append(out, OverviewPoolEntryDTO{
				ID: id, Name: name, Kind: "virtual", Weight: weight,
				Available: count > 0, MemberCount: count, Platform: "openai", Type: "oauth_pool",
			})
			continue
		}
		account, ok := accountByID[id]
		if !ok {
			out = append(out, OverviewPoolEntryDTO{ID: id, Name: fmt.Sprintf("账号 #%d", id), Kind: "account", Weight: weight})
			continue
		}
		entry := OverviewPoolEntryDTO{
			ID: account.ID, Name: account.Name, Kind: "account", Weight: weight,
			Available: isOverviewAccountAvailable(account), Platform: account.Platform, Type: account.Type,
			Status: normalizeOverviewStatus(account.Status), Schedulable: account.Schedulable,
			Concurrency: account.Concurrency, Priority: account.Priority, RateMultiplier: account.RateMultiplier,
			ManagedSyncGroupNames: append([]string(nil), managedNames[account.ID]...),
		}
		entry.Managed = len(entry.ManagedSyncGroupNames) > 0
		if account.ProxyID != nil {
			entry.ProxyName = proxyNames[*account.ProxyID]
			if entry.ProxyName == "" {
				entry.ProxyName = fmt.Sprintf("代理 #%d", *account.ProxyID)
			}
		}
		out = append(out, entry)
	}
	return out
}

func countVirtualOAuthPools(accounts []sub2api.AdminAccount, now time.Time) map[int64]int {
	counts := make(map[int64]int, len(virtualOAuthPoolNames))
	for id := range virtualOAuthPoolNames {
		counts[id] = 0
	}
	for _, account := range accounts {
		if !strings.EqualFold(account.Platform, "openai") || !strings.EqualFold(account.Type, "oauth") || !isOverviewAccountCurrentlySchedulable(account, now) {
			continue
		}
		if id, ok := classifyVirtualOAuthPool(account.Credentials["plan_type"]); ok {
			counts[id]++
		}
	}
	return counts
}

func classifyVirtualOAuthPool(value any) (int64, bool) {
	plan := normalizeSmartDispatchPlan(fmt.Sprint(value))
	if plan == "" || hasSmartDispatchPlanToken(plan, "abnormal") {
		return 0, false
	}
	switch {
	case hasSmartDispatchPlanToken(plan, "free", "chatgptfree"):
		return virtualOAuthFreeID, true
	case hasSmartDispatchPlanToken(plan, "k12", "edu", "education", "school", "student", "teacher", "chatgptk12"):
		return virtualOAuthK12ID, true
	case hasSmartDispatchPlanToken(plan, "team", "business", "workspace", "enterprise", "chatgptteam"):
		return virtualOAuthTeamID, true
	case hasSmartDispatchPlanToken(plan, "pro", "chatgptpro"):
		return virtualOAuthProID, true
	case hasSmartDispatchPlanToken(plan, "plus", "chatgptplus"):
		return virtualOAuthPlusID, true
	default:
		return 0, false
	}
}

func normalizeSmartDispatchPlan(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	separator := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if separator && out.Len() > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(char)
			separator = false
		} else {
			separator = true
		}
	}
	return strings.Trim(out.String(), "_")
}

func hasSmartDispatchPlanToken(plan string, tokens ...string) bool {
	padded := "_" + plan + "_"
	for _, token := range tokens {
		token = normalizeSmartDispatchPlan(token)
		if token != "" && (plan == token || strings.Contains(padded, "_"+token+"_")) {
			return true
		}
	}
	return false
}

func isOverviewAccountCurrentlySchedulable(account sub2api.AdminAccount, now time.Time) bool {
	if !isOverviewAccountAvailable(account) {
		return false
	}
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && *account.ExpiresAt > 0 && time.Unix(*account.ExpiresAt, 0).Before(now.Add(time.Second)) {
		return false
	}
	for _, value := range []string{account.OverloadUntil, account.RateLimitResetAt, account.TempUnschedulableUntil} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil && parsed.After(now) {
			return false
		}
	}
	return true
}

func isOverviewAccountAvailable(account sub2api.AdminAccount) bool {
	return normalizeOverviewStatus(account.Status) == "active" && account.Schedulable
}

func normalizeSmartDispatchWeight(value int64) int {
	if value < 1 {
		return 1
	}
	if value > 999 {
		return 999
	}
	return int(value)
}

func overviewGroupSort(group sub2api.AdminGroup) int {
	if group.SortOrder != 0 {
		return group.SortOrder
	}
	return group.Sort
}

func (s *Service) SetOverviewAccountSchedulable(ctx context.Context, accountID int64, schedulable bool) (*SchedulableUpdateDTO, error) {
	if accountID <= 0 {
		return nil, errors.New("虚拟池不能直接修改调度状态")
	}
	_, adminTarget, err := s.overviewTarget()
	if err != nil {
		return nil, err
	}
	account, err := sub2api.NewAdminClient().SetAccountSchedulable(ctx, adminTarget, accountID, schedulable)
	if err != nil {
		return nil, fmt.Errorf("更新 Sub2API 上游账号调度失败: %w", err)
	}
	return &SchedulableUpdateDTO{AccountID: accountID, Schedulable: account.Schedulable}, nil
}

func (s *Service) UpdateOverviewGroupSmartRouting(ctx context.Context, groupID int64, input SmartRoutingUpdateInput) (*SmartRoutingUpdateDTO, error) {
	if groupID <= 0 {
		return nil, fmt.Errorf("%w: group_id 必须大于 0", ErrInvalidOverviewRouting)
	}
	if err := validateSmartRoutingPool(input.PrimaryPool); err != nil {
		return nil, fmt.Errorf("%w: 主调度池: %v", ErrInvalidOverviewRouting, err)
	}
	if err := validateSmartRoutingPool(input.FallbackPool); err != nil {
		return nil, fmt.Errorf("%w: 保底池: %v", ErrInvalidOverviewRouting, err)
	}
	_, adminTarget, err := s.overviewTarget()
	if err != nil {
		return nil, err
	}
	client := sub2api.NewAdminClient()
	groups, err := client.ListGroups(ctx, adminTarget, true)
	if err != nil {
		return nil, fmt.Errorf("读取 Sub2API 分组失败: %w", err)
	}
	var current *sub2api.AdminGroup
	for i := range groups {
		if groups[i].ID == groupID {
			current = &groups[i]
			break
		}
	}
	if current == nil {
		return nil, fmt.Errorf("%w: 分组 #%d 不存在", ErrInvalidOverviewRouting, groupID)
	}
	if err := validateSmartRoutingPoolIDs(input.PrimaryPool, current.ModelRouting["*"]); err != nil {
		return nil, fmt.Errorf("%w: 主调度池: %v", ErrInvalidOverviewRouting, err)
	}
	if err := validateSmartRoutingPoolIDs(input.FallbackPool, current.ModelRouting[smartDispatchFallbackKey]); err != nil {
		return nil, fmt.Errorf("%w: 保底池: %v", ErrInvalidOverviewRouting, err)
	}
	routing := cloneModelRouting(current.ModelRouting)
	routing[smartDispatchPrimaryWeightsKey] = smartRoutingWeights(input.PrimaryPool)
	routing[smartDispatchFallbackWeightsKey] = smartRoutingWeights(input.FallbackPool)
	if _, err := client.UpdateGroupRouting(ctx, adminTarget, groupID, sub2api.AdminGroupRoutingUpdate{
		ModelRouting: routing, ModelRoutingEnabled: current.ModelRoutingEnabled,
	}); err != nil {
		return nil, fmt.Errorf("更新 Sub2API 智能调度路由失败: %w", err)
	}
	return &SmartRoutingUpdateDTO{
		GroupID: groupID, SmartDispatchEnabled: current.ModelRoutingEnabled,
		PrimaryPool:  append([]SmartRoutingEntryInput(nil), input.PrimaryPool...),
		FallbackPool: append([]SmartRoutingEntryInput(nil), input.FallbackPool...),
	}, nil
}

func validateSmartRoutingPool(entries []SmartRoutingEntryInput) error {
	for i, entry := range entries {
		if entry.ID == 0 {
			return fmt.Errorf("第 %d 项账号 ID 不能为 0", i+1)
		}
		if entry.ID < 0 {
			if _, ok := virtualOAuthPoolNames[entry.ID]; !ok {
				return fmt.Errorf("第 %d 项虚拟池 ID 不受支持", i+1)
			}
		}
		if entry.Weight < 1 || entry.Weight > 999 {
			return fmt.Errorf("第 %d 项权重必须在 1-999 之间", i+1)
		}
	}
	return nil
}

func validateSmartRoutingPoolIDs(entries []SmartRoutingEntryInput, expected []int64) error {
	if len(entries) != len(expected) {
		return errors.New("账号池已变更，请刷新后重试")
	}
	for i, entry := range entries {
		if entry.ID != expected[i] {
			return errors.New("账号池已变更，请刷新后重试")
		}
	}
	return nil
}

func cloneModelRouting(source map[string][]int64) map[string][]int64 {
	out := make(map[string][]int64, len(source)+4)
	for key, values := range source {
		out[key] = append([]int64(nil), values...)
	}
	return out
}

func smartRoutingWeights(entries []SmartRoutingEntryInput) []int64 {
	out := make([]int64, len(entries))
	for i, entry := range entries {
		out[i] = int64(entry.Weight)
	}
	return out
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

func normalizeOverviewStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "active"
	}
	return status
}
