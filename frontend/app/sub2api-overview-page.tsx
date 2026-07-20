import { useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  AlertCircle,
  Database,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  Save,
  Search,
  Server,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { apiFetch, type ApiError } from "@/lib/api"
import type {
  Sub2APIOverview,
  Sub2APIOverviewGroup,
  Sub2APIOverviewPoolEntry,
  Sub2APISchedulableUpdate,
  Sub2APISmartRoutingEntry,
  Sub2APISmartRoutingUpdate,
} from "@/lib/api-types"
import { cn } from "@/lib/utils"

const ALL = "all"

interface GroupWeightDraft {
  primary: string[]
  fallback: string[]
}

export default function Sub2APIOverviewPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<Sub2APIOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")
  const [errorStatus, setErrorStatus] = useState<number | null>(null)
  const [busyAccounts, setBusyAccounts] = useState<Set<number>>(new Set())
  const [busyGroups, setBusyGroups] = useState<Set<number>>(new Set())
  const [weightDrafts, setWeightDrafts] = useState<Record<number, GroupWeightDraft>>({})
  const [activeGroup, setActiveGroup] = useState("")
  const [search, setSearch] = useState("")
  const [platform, setPlatform] = useState(ALL)
  const [dispatch, setDispatch] = useState(ALL)

  useEffect(() => {
    const controller = new AbortController()
    void loadOverview(false, controller.signal)
    return () => controller.abort()
  }, [])

  async function loadOverview(manual: boolean, signal?: AbortSignal) {
    if (manual) setRefreshing(true)
    else setLoading(true)
    try {
      const result = await apiFetch<Sub2APIOverview>("/upstream-sync/overview", { signal })
      setData(result)
      setWeightDrafts(createWeightDrafts(result.groups))
      setError("")
      setErrorStatus(null)
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return
      const apiError = err as ApiError
      setError(err instanceof Error ? err.message : "聚合数据加载失败")
      setErrorStatus(typeof apiError.status === "number" ? apiError.status : null)
    } finally {
      if (!signal?.aborted) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }

  async function updateSchedulable(entry: Sub2APIOverviewPoolEntry, next: boolean) {
    if (entry.kind !== "account" || entry.id <= 0) return
    setBusyAccounts((current) => new Set(current).add(entry.id))
    try {
      const result = await apiFetch<Sub2APISchedulableUpdate>(
        `/upstream-sync/accounts/${entry.id}/schedulable`,
        { method: "PUT", body: JSON.stringify({ schedulable: next }) },
      )
      setData((current) => current ? applySchedulableUpdate(current, result.account_id, result.schedulable) : current)
      toast.success(result.schedulable ? "已启用真实上游调度" : "已禁用真实上游调度")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "更新真实上游调度失败")
    } finally {
      setBusyAccounts((current) => removeFromSet(current, entry.id))
    }
  }

  function updateWeight(groupID: number, pool: "primary" | "fallback", index: number, value: string) {
    setWeightDrafts((current) => {
      const group = data?.groups.find((item) => item.id === groupID)
      const previous = current[groupID] ?? (group ? createGroupWeightDraft(group) : { primary: [], fallback: [] })
      const values = [...previous[pool]]
      values[index] = value
      return { ...current, [groupID]: { ...previous, [pool]: values } }
    })
  }

  async function saveWeights(group: Sub2APIOverviewGroup) {
    const draft = weightDrafts[group.id] ?? createGroupWeightDraft(group)
    const primaryPool = buildRoutingEntries(group.primary_pool, draft.primary)
    const fallbackPool = buildRoutingEntries(group.fallback_pool, draft.fallback)
    if (!primaryPool || !fallbackPool) {
      toast.error("权重必须是 1-999 之间的整数")
      return
    }
    setBusyGroups((current) => new Set(current).add(group.id))
    try {
      const result = await apiFetch<Sub2APISmartRoutingUpdate>(
        `/upstream-sync/groups/${group.id}/smart-routing`,
        { method: "PUT", body: JSON.stringify({ primary_pool: primaryPool, fallback_pool: fallbackPool }) },
      )
      setData((current) => current ? applySmartRoutingUpdate(current, result) : current)
      setWeightDrafts((current) => ({
        ...current,
        [group.id]: {
          primary: result.primary_pool.map((entry) => String(entry.weight)),
          fallback: result.fallback_pool.map((entry) => String(entry.weight)),
        },
      }))
      toast.success(`已保存“${group.name}”的调度权重`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "保存调度权重失败")
    } finally {
      setBusyGroups((current) => removeFromSet(current, group.id))
    }
  }

  const platforms = useMemo(
    () => uniqueSorted(data?.groups.map((group) => group.platform || "").filter(Boolean) ?? []),
    [data],
  )
  const filteredGroups = useMemo(() => {
    if (!data) return []
    const needle = search.trim().toLocaleLowerCase()
    return data.groups.filter((group) => {
      if (platform !== ALL && group.platform !== platform) return false
      if (dispatch === "enabled" && !group.smart_dispatch_enabled) return false
      if (dispatch === "disabled" && group.smart_dispatch_enabled) return false
      if (!needle) return true
      const poolNames = [...group.primary_pool, ...group.fallback_pool].map((entry) => entry.name).join(" ")
      return `${group.name} ${group.id} ${poolNames}`.toLocaleLowerCase().includes(needle)
    })
  }, [data, dispatch, platform, search])

  useEffect(() => {
    if (filteredGroups.length === 0) {
      if (activeGroup) setActiveGroup("")
      return
    }
    if (!filteredGroups.some((group) => String(group.id) === activeGroup)) {
      setActiveGroup(String(filteredGroups[0].id))
    }
  }, [activeGroup, filteredGroups])

  const selectedGroup = filteredGroups.find((group) => String(group.id) === activeGroup)
  const filtersActive = Boolean(search || platform !== ALL || dispatch !== ALL)

  function resetFilters() {
    setSearch("")
    setPlatform(ALL)
    setDispatch(ALL)
  }

  if (loading && !data) return <OverviewSkeleton />
  if (!data) {
    const unconfigured = errorStatus === 409
    return (
      <Empty className="min-h-96 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">{unconfigured ? <Server /> : <AlertCircle />}</EmptyMedia>
          <EmptyTitle>{unconfigured ? "尚未配置唯一的 Sub2API 目标" : "无法加载智能调度聚合"}</EmptyTitle>
          <EmptyDescription>{error}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          {unconfigured ? (
            <Button onClick={() => navigate("/settings")}>前往系统设置</Button>
          ) : (
            <Button variant="outline" onClick={() => void loadOverview(true)} disabled={refreshing}>
              <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
              重新加载
            </Button>
          )}
        </EmptyContent>
      </Empty>
    )
  }

  return (
    <section className="space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-lg font-semibold text-foreground">Sub2API 智能调度聚合</h1>
            <Badge variant="outline">{data.target.name}</Badge>
            {!data.target.enabled ? <Badge variant="destructive">目标已禁用</Badge> : null}
          </div>
          <p className="break-all text-xs text-muted-foreground">{data.target.base_url}</p>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="icon" aria-label="刷新聚合数据" disabled={refreshing} onClick={() => void loadOverview(true)}>
              <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>刷新聚合数据</TooltipContent>
        </Tooltip>
      </header>

      {error ? (
        <Alert variant="destructive">
          <AlertCircle />
          <AlertTitle>刷新失败</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-foreground">分组账号池</h2>
            <Badge variant="secondary" className="font-mono">{filteredGroups.length}/{data.groups.length}</Badge>
          </div>
          <div className="grid w-full gap-2 sm:w-auto sm:grid-cols-[minmax(220px,280px)_150px_170px_36px]">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索分组或账号池" className="pl-9" />
            </div>
            <FilterSelect value={platform} onChange={setPlatform} placeholder="全部平台" items={platforms} />
            <FilterSelect value={dispatch} onChange={setDispatch} placeholder="全部调度状态" items={[{ value: "enabled", label: "已启用智能调度" }, { value: "disabled", label: "未启用智能调度" }]} />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon" aria-label="重置筛选" disabled={!filtersActive} onClick={resetFilters}><RotateCcw className="size-4" /></Button>
              </TooltipTrigger>
              <TooltipContent>重置筛选</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {filteredGroups.length === 0 ? (
          <Empty className="min-h-48 border">
            <EmptyHeader>
              <EmptyMedia variant="icon"><Search /></EmptyMedia>
              <EmptyTitle>没有匹配的分组账号池</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <Tabs value={activeGroup} onValueChange={setActiveGroup} className="min-w-0 gap-3">
            <TabsList className="h-auto w-full justify-start gap-1 overflow-x-auto p-1">
              {filteredGroups.map((group) => (
                <TabsTrigger key={group.id} value={String(group.id)} className="h-9 flex-none px-3">
                  <span className="max-w-44 truncate">{group.name}</span>
                  <span className="text-[10px] tabular-nums text-muted-foreground">{group.primary_pool.length + group.fallback_pool.length}</span>
                </TabsTrigger>
              ))}
            </TabsList>
            {selectedGroup ? (
              <TabsContent value={String(selectedGroup.id)}>
                <GroupCard
                  group={selectedGroup}
                  draft={weightDrafts[selectedGroup.id] ?? createGroupWeightDraft(selectedGroup)}
                  busyAccounts={busyAccounts}
                  saving={busyGroups.has(selectedGroup.id)}
                  onToggle={updateSchedulable}
                  onWeightChange={updateWeight}
                  onSave={() => void saveWeights(selectedGroup)}
                />
              </TabsContent>
            ) : null}
          </Tabs>
        )}
      </section>
    </section>
  )
}

function GroupCard({
  group,
  draft,
  busyAccounts,
  saving,
  onToggle,
  onWeightChange,
  onSave,
}: {
  group: Sub2APIOverviewGroup
  draft: GroupWeightDraft
  busyAccounts: Set<number>
  saving: boolean
  onToggle: (entry: Sub2APIOverviewPoolEntry, next: boolean) => void
  onWeightChange: (groupID: number, pool: "primary" | "fallback", index: number, value: string) => void
  onSave: () => void
}) {
  const dirty = isWeightDraftDirty(group, draft)
  return (
    <Card className="min-w-0 gap-0 overflow-hidden rounded-lg py-0 shadow-none">
      <CardContent className="space-y-5 p-4 sm:p-5">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="truncate text-base font-semibold text-foreground" title={group.name}>{group.name}</h3>
              <Badge variant={group.smart_dispatch_enabled ? "default" : "outline"}>
                {group.smart_dispatch_enabled ? "智能调度" : "普通调度"}
              </Badge>
            </div>
            <p className="mt-1 text-xs text-muted-foreground">{group.platform || "未指定平台"} · 倍率 {formatNumber(group.ratio)} · #{group.id}</p>
            <p className="mt-1 text-[11px] text-muted-foreground">权重范围 1-999；保存仅更新当前分组的智能调度路由。</p>
          </div>
          <Button size="sm" disabled={!dirty || saving} onClick={onSave}>
            {saving ? <LoaderCircle className="size-4 animate-spin" /> : <Save className="size-4" />}
            {saving ? "保存中" : "保存权重"}
          </Button>
        </div>
        <div className="grid gap-5 lg:grid-cols-2">
          <PoolSection title="主调度池" pool="primary" groupID={group.id} entries={group.primary_pool} weights={draft.primary} busyAccounts={busyAccounts} onToggle={onToggle} onWeightChange={onWeightChange} />
          <PoolSection title="保底池" pool="fallback" groupID={group.id} entries={group.fallback_pool} weights={draft.fallback} busyAccounts={busyAccounts} onToggle={onToggle} onWeightChange={onWeightChange} />
        </div>
      </CardContent>
    </Card>
  )
}

function PoolSection({
  title,
  pool,
  groupID,
  entries,
  weights,
  busyAccounts,
  onToggle,
  onWeightChange,
}: {
  title: string
  pool: "primary" | "fallback"
  groupID: number
  entries: Sub2APIOverviewPoolEntry[]
  weights: string[]
  busyAccounts: Set<number>
  onToggle: (entry: Sub2APIOverviewPoolEntry, next: boolean) => void
  onWeightChange: (groupID: number, pool: "primary" | "fallback", index: number, value: string) => void
}) {
  return (
    <div className="min-w-0 space-y-2">
      <div className="flex items-center gap-2 border-b border-border pb-2">
        <h4 className="text-sm font-semibold text-foreground">{title}</h4>
        <Badge variant="secondary" className="font-mono">{entries.length}</Badge>
      </div>
      {entries.length === 0 ? (
        <p className="py-5 text-center text-xs text-muted-foreground">未配置账号池</p>
      ) : (
        <div className="divide-y divide-border">
          {entries.map((entry, index) => (
            <PoolEntryRow
              key={`${entry.kind}-${entry.id}-${index}`}
              entry={entry}
              weight={weights[index] ?? String(entry.weight)}
              busy={busyAccounts.has(entry.id)}
              onToggle={onToggle}
              onWeightChange={(value) => onWeightChange(groupID, pool, index, value)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function PoolEntryRow({
  entry,
  weight,
  busy,
  onToggle,
  onWeightChange,
}: {
  entry: Sub2APIOverviewPoolEntry
  weight: string
  busy: boolean
  onToggle: (entry: Sub2APIOverviewPoolEntry, next: boolean) => void
  onWeightChange: (value: string) => void
}) {
  const virtual = entry.kind === "virtual"
  const validWeight = isValidWeight(weight)
  return (
    <div className="flex min-w-0 flex-wrap items-start gap-2 py-3 first:pt-1 last:pb-1 sm:flex-nowrap">
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
        {virtual ? <Database className="size-4" /> : <Server className="size-4" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <p className="max-w-full truncate text-xs font-medium text-foreground" title={entry.name}>{entry.name}</p>
          <Badge variant="outline" className="shrink-0 text-[10px]">{virtual ? "虚拟池" : "真实上游"}</Badge>
          {entry.managed ? <Badge variant="secondary" className="max-w-28 truncate text-[10px]">托管</Badge> : null}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
          {virtual ? (
            <span className={entry.available ? "text-emerald-700 dark:text-emerald-300" : "text-muted-foreground"}>
              {entry.member_count ?? 0} 个可用授权账号
            </span>
          ) : (
            <>
              <span>{entry.platform || "未知平台"}</span>
              {entry.concurrency ? <span>并发 {entry.concurrency}</span> : null}
              {entry.proxy_name ? <span>代理 {entry.proxy_name}</span> : null}
            </>
          )}
        </div>
      </div>
      <div className="ml-10 flex shrink-0 items-center gap-3 sm:ml-0">
        {virtual ? (
          <Badge variant={entry.available ? "outline" : "secondary"} className="text-[10px]">{entry.available ? "可用" : "空池"}</Badge>
        ) : (
          <div className="flex items-center gap-1.5">
            <Switch checked={entry.schedulable} disabled={busy} aria-label={`${entry.name}调度`} onCheckedChange={(next) => onToggle(entry, next)} />
            <span className="hidden text-[11px] text-muted-foreground sm:inline">{busy ? "更新中" : entry.schedulable ? "可调度" : "已停用"}</span>
          </div>
        )}
        <label className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          权重
          <Input
            type="number"
            min={1}
            max={999}
            step={1}
            value={weight}
            aria-label={`${entry.name}权重`}
            aria-invalid={!validWeight}
            onChange={(event) => onWeightChange(event.target.value)}
            className={cn("h-8 w-20 px-2 text-right font-mono text-xs", !validWeight && "border-destructive focus-visible:ring-destructive/30")}
          />
        </label>
      </div>
    </div>
  )
}

type FilterItem = string | { value: string; label: string }

function FilterSelect({ value, onChange, placeholder, items }: { value: string; onChange: (value: string) => void; placeholder: string; items: FilterItem[] }) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{placeholder}</SelectItem>
        {items.map((raw) => {
          const item = typeof raw === "string" ? { value: raw, label: raw } : raw
          return <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
        })}
      </SelectContent>
    </Select>
  )
}

function OverviewSkeleton() {
  return (
    <section className="space-y-5">
      <div className="flex items-center justify-between"><div className="space-y-2"><Skeleton className="h-6 w-52" /><Skeleton className="h-3 w-64" /></div><Skeleton className="size-9" /></div>
      <Skeleton className="h-10 w-full" />
      <Skeleton className="h-72 w-full" />
    </section>
  )
}

function createGroupWeightDraft(group: Sub2APIOverviewGroup): GroupWeightDraft {
  return {
    primary: group.primary_pool.map((entry) => String(entry.weight)),
    fallback: group.fallback_pool.map((entry) => String(entry.weight)),
  }
}

function createWeightDrafts(groups: Sub2APIOverviewGroup[]) {
  return Object.fromEntries(groups.map((group) => [group.id, createGroupWeightDraft(group)])) as Record<number, GroupWeightDraft>
}

function isValidWeight(value: string) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 999
}

function buildRoutingEntries(entries: Sub2APIOverviewPoolEntry[], weights: string[]): Sub2APISmartRoutingEntry[] | null {
  const result: Sub2APISmartRoutingEntry[] = []
  for (let index = 0; index < entries.length; index += 1) {
    const raw = weights[index] ?? String(entries[index].weight)
    if (!isValidWeight(raw)) return null
    result.push({ id: entries[index].id, weight: Number(raw) })
  }
  return result
}

function isWeightDraftDirty(group: Sub2APIOverviewGroup, draft: GroupWeightDraft) {
  return group.primary_pool.some((entry, index) => draft.primary[index] !== String(entry.weight))
    || group.fallback_pool.some((entry, index) => draft.fallback[index] !== String(entry.weight))
}

function applySchedulableUpdate(data: Sub2APIOverview, accountID: number, schedulable: boolean): Sub2APIOverview {
  const updatePool = (entries: Sub2APIOverviewPoolEntry[]) => entries.map((entry) => {
    if (entry.kind !== "account" || entry.id !== accountID) return entry
    return { ...entry, schedulable, available: schedulable && entry.status?.toLocaleLowerCase() === "active" }
  })
  return {
    ...data,
    groups: data.groups.map((group) => ({ ...group, primary_pool: updatePool(group.primary_pool), fallback_pool: updatePool(group.fallback_pool) })),
  }
}

function applySmartRoutingUpdate(data: Sub2APIOverview, update: Sub2APISmartRoutingUpdate): Sub2APIOverview {
  return {
    ...data,
    groups: data.groups.map((group) => group.id !== update.group_id ? group : {
      ...group,
      smart_dispatch_enabled: update.smart_dispatch_enabled,
      primary_pool: group.primary_pool.map((entry, index) => ({ ...entry, weight: update.primary_pool[index]?.weight ?? entry.weight })),
      fallback_pool: group.fallback_pool.map((entry, index) => ({ ...entry, weight: update.fallback_pool[index]?.weight ?? entry.weight })),
    }),
  }
}

function removeFromSet(current: Set<number>, value: number) {
  const next = new Set(current)
  next.delete(value)
  return next
}

function uniqueSorted(items: string[]) {
  return [...new Set(items)].sort((a, b) => a.localeCompare(b))
}

function formatNumber(value: number) {
  return Number.isFinite(value) ? value.toLocaleString("zh-CN", { maximumFractionDigits: 6 }) : "-"
}
