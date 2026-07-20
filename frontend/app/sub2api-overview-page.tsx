import { useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import {
  AlertCircle,
  CheckCircle2,
  CircleGauge,
  Link2,
  RefreshCw,
  RotateCcw,
  Search,
  Server,
  Users,
} from "lucide-react"
import { toast } from "sonner"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { apiFetch, type ApiError } from "@/lib/api"
import type {
  Sub2APIOverview,
  Sub2APIOverviewAccount,
  Sub2APIOverviewGroup,
  Sub2APISchedulableUpdate,
} from "@/lib/api-types"
import { cn } from "@/lib/utils"

const ALL = "all"

export default function Sub2APIOverviewPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<Sub2APIOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState("")
  const [errorStatus, setErrorStatus] = useState<number | null>(null)
  const [busyAccounts, setBusyAccounts] = useState<Set<number>>(new Set())
  const [search, setSearch] = useState("")
  const [platform, setPlatform] = useState(ALL)
  const [group, setGroup] = useState(ALL)
  const [status, setStatus] = useState(ALL)
  const [schedulable, setSchedulable] = useState(ALL)
  const [managed, setManaged] = useState(ALL)

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

  async function updateSchedulable(account: Sub2APIOverviewAccount, next: boolean) {
    setBusyAccounts((current) => new Set(current).add(account.id))
    try {
      const result = await apiFetch<Sub2APISchedulableUpdate>(
        `/upstream-sync/accounts/${account.id}/schedulable`,
        { method: "PUT", body: JSON.stringify({ schedulable: next }) },
      )
      setData((current) => current ? applySchedulableUpdate(current, result.account_id, result.schedulable) : current)
      toast.success(result.schedulable ? "已启用账号调度" : "已禁用账号调度")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "更新账号调度失败")
    } finally {
      setBusyAccounts((current) => {
        const nextSet = new Set(current)
        nextSet.delete(account.id)
        return nextSet
      })
    }
  }

  const platforms = useMemo(
    () => uniqueSorted(data?.accounts.map((account) => account.platform).filter(Boolean) ?? []),
    [data],
  )
  const statuses = useMemo(
    () => uniqueSorted(data?.accounts.map((account) => account.status).filter(Boolean) ?? []),
    [data],
  )
  const filteredAccounts = useMemo(() => {
    if (!data) return []
    const needle = search.trim().toLocaleLowerCase()
    return data.accounts.filter((account) => {
      if (needle && !`${account.name} ${account.id}`.toLocaleLowerCase().includes(needle)) return false
      if (platform !== ALL && account.platform !== platform) return false
      if (group !== ALL) {
        if (group === "ungrouped" && account.groups.length > 0) return false
        if (group !== "ungrouped" && !account.groups.some((item) => String(item.id) === group)) return false
      }
      if (status !== ALL && account.status !== status) return false
      if (schedulable === "enabled" && !account.schedulable) return false
      if (schedulable === "disabled" && account.schedulable) return false
      if (managed === "managed" && !account.managed) return false
      if (managed === "unmanaged" && account.managed) return false
      return true
    })
  }, [data, group, managed, platform, schedulable, search, status])

  const filtersActive = Boolean(
    search || platform !== ALL || group !== ALL || status !== ALL || schedulable !== ALL || managed !== ALL,
  )

  function resetFilters() {
    setSearch("")
    setPlatform(ALL)
    setGroup(ALL)
    setStatus(ALL)
    setSchedulable(ALL)
    setManaged(ALL)
  }

  if (loading && !data) return <OverviewSkeleton />

  if (!data) {
    const unconfigured = errorStatus === 409
    return (
      <Empty className="min-h-96 border">
        <EmptyHeader>
          <EmptyMedia variant="icon">{unconfigured ? <Server /> : <AlertCircle />}</EmptyMedia>
          <EmptyTitle>{unconfigured ? "尚未配置唯一的 Sub2API 目标" : "无法加载 Sub2API 聚合数据"}</EmptyTitle>
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
            <h1 className="text-lg font-semibold text-foreground">Sub2API 聚合</h1>
            <Badge variant="outline">{data.target.name}</Badge>
            {!data.target.enabled ? <Badge variant="destructive">目标已禁用</Badge> : null}
          </div>
          <p className="break-all text-xs text-muted-foreground">{data.target.base_url}</p>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              aria-label="刷新聚合数据"
              disabled={refreshing}
              onClick={() => void loadOverview(true)}
            >
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

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        <MetricCard icon={<Users />} label="账号总数" value={data.summary.total_accounts} />
        <MetricCard icon={<CheckCircle2 />} label="启用账号" value={data.summary.active_accounts} />
        <MetricCard icon={<CircleGauge />} label="可调度" value={data.summary.schedulable_accounts} />
        <MetricCard icon={<Link2 />} label="托管账号" value={data.summary.managed_accounts} />
        <MetricCard icon={<Server />} label="非托管" value={data.summary.unmanaged_accounts} />
      </div>

      <section className="space-y-2">
        <SectionHeading title="分组汇总" count={data.groups.length} />
        <GroupSummary groups={data.groups} />
      </section>

      <section className="space-y-3">
        <SectionHeading title="账号" count={filteredAccounts.length} total={data.accounts.length} />
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-[minmax(220px,1.4fr)_repeat(5,minmax(130px,1fr))_36px]">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="搜索账号名称或 ID"
              className="pl-9"
            />
          </div>
          <FilterSelect value={platform} onChange={setPlatform} placeholder="全部平台" items={platforms} />
          <FilterSelect
            value={group}
            onChange={setGroup}
            placeholder="全部分组"
            items={data.groups.map((item) => ({ value: item.id === 0 ? "ungrouped" : String(item.id), label: item.name }))}
          />
          <FilterSelect
            value={status}
            onChange={setStatus}
            placeholder="全部状态"
            items={statuses.map((item) => ({ value: item, label: statusLabel(item) }))}
          />
          <FilterSelect
            value={schedulable}
            onChange={setSchedulable}
            placeholder="全部调度"
            items={[{ value: "enabled", label: "可调度" }, { value: "disabled", label: "不可调度" }]}
          />
          <FilterSelect
            value={managed}
            onChange={setManaged}
            placeholder="全部来源"
            items={[{ value: "managed", label: "托管账号" }, { value: "unmanaged", label: "非托管账号" }]}
          />
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="outline"
                size="icon"
                aria-label="重置筛选"
                disabled={!filtersActive}
                onClick={resetFilters}
              >
                <RotateCcw className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>重置筛选</TooltipContent>
          </Tooltip>
        </div>

        {filteredAccounts.length === 0 ? (
          <Empty className="min-h-48 border">
            <EmptyHeader>
              <EmptyMedia variant="icon"><Search /></EmptyMedia>
              <EmptyTitle>没有匹配的账号</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <>
            <AccountTable accounts={filteredAccounts} busyAccounts={busyAccounts} onToggle={updateSchedulable} />
            <AccountCards accounts={filteredAccounts} busyAccounts={busyAccounts} onToggle={updateSchedulable} />
          </>
        )}
      </section>
    </section>
  )
}

function MetricCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: number }) {
  return (
    <Card className="gap-2 rounded-lg py-3 shadow-none">
      <CardContent className="flex items-center gap-3 px-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground [&>svg]:size-4">{icon}</span>
        <div className="min-w-0">
          <p className="truncate text-xs text-muted-foreground">{label}</p>
          <p className="text-xl font-semibold tabular-nums text-foreground">{value}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function SectionHeading({ title, count, total }: { title: string; count: number; total?: number }) {
  return (
    <div className="flex items-center gap-2">
      <h2 className="text-sm font-semibold text-foreground">{title}</h2>
      <Badge variant="secondary" className="font-mono">
        {total != null && total !== count ? `${count}/${total}` : count}
      </Badge>
    </div>
  )
}

function GroupSummary({ groups }: { groups: Sub2APIOverviewGroup[] }) {
  return (
    <>
      <div className="hidden overflow-hidden rounded-lg border border-border sm:block">
        <Table>
          <TableHeader className="bg-muted/40">
            <TableRow>
              <TableHead>分组</TableHead>
              <TableHead>平台</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">倍率</TableHead>
              <TableHead className="text-right">账号</TableHead>
              <TableHead className="text-right">启用</TableHead>
              <TableHead className="text-right">可调度</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((item) => (
              <TableRow key={item.id || "ungrouped"}>
                <TableCell className="font-medium">{item.name}</TableCell>
                <TableCell>{item.platform || "-"}</TableCell>
                <TableCell><StatusBadge status={item.status} /></TableCell>
                <TableCell className="text-right font-mono">{formatNumber(item.ratio)}</TableCell>
                <TableCell className="text-right tabular-nums">{item.account_count}</TableCell>
                <TableCell className="text-right tabular-nums">{item.active_account_count}</TableCell>
                <TableCell className="text-right tabular-nums">{item.schedulable_account_count}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className="grid gap-2 sm:hidden">
        {groups.map((item) => (
          <Card key={item.id || "ungrouped"} className="gap-3 rounded-lg py-3 shadow-none">
            <CardContent className="space-y-3 px-3">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="break-words text-sm font-medium text-foreground">{item.name}</p>
                  <p className="text-xs text-muted-foreground">{item.platform || "未指定平台"} · 倍率 {formatNumber(item.ratio)}</p>
                </div>
                <StatusBadge status={item.status} />
              </div>
              <div className="grid grid-cols-3 gap-2 text-center text-xs">
                <GroupCount label="账号" value={item.account_count} />
                <GroupCount label="启用" value={item.active_account_count} />
                <GroupCount label="可调度" value={item.schedulable_account_count} />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  )
}

function GroupCount({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md bg-muted/60 px-2 py-1.5">
      <p className="text-muted-foreground">{label}</p>
      <p className="font-mono text-sm font-semibold text-foreground">{value}</p>
    </div>
  )
}

type FilterItem = string | { value: string; label: string }

function FilterSelect({
  value,
  onChange,
  placeholder,
  items,
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  items: FilterItem[]
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="w-full">
        <SelectValue />
      </SelectTrigger>
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

function AccountTable({
  accounts,
  busyAccounts,
  onToggle,
}: {
  accounts: Sub2APIOverviewAccount[]
  busyAccounts: Set<number>
  onToggle: (account: Sub2APIOverviewAccount, next: boolean) => void
}) {
  return (
    <div className="hidden overflow-hidden rounded-lg border border-border md:block">
      <Table>
        <TableHeader className="bg-muted/40">
          <TableRow>
            <TableHead>账号</TableHead>
            <TableHead>分组</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>调度</TableHead>
            <TableHead className="text-right">倍率</TableHead>
            <TableHead className="text-right">并发 / 优先级</TableHead>
            <TableHead>代理</TableHead>
            <TableHead>来源</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {accounts.map((account) => (
            <TableRow key={account.id}>
              <TableCell>
                <AccountIdentity account={account} />
              </TableCell>
              <TableCell className="max-w-72 whitespace-normal"><GroupBadges account={account} /></TableCell>
              <TableCell><StatusBadge status={account.status} /></TableCell>
              <TableCell>
                <ScheduleSwitch account={account} busy={busyAccounts.has(account.id)} onToggle={onToggle} />
              </TableCell>
              <TableCell className="text-right font-mono">{formatNumber(account.rate_multiplier)}</TableCell>
              <TableCell className="text-right font-mono">{account.concurrency} / {account.priority}</TableCell>
              <TableCell>{account.proxy_name || "-"}</TableCell>
              <TableCell><ManagedBadge account={account} /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function AccountCards({
  accounts,
  busyAccounts,
  onToggle,
}: {
  accounts: Sub2APIOverviewAccount[]
  busyAccounts: Set<number>
  onToggle: (account: Sub2APIOverviewAccount, next: boolean) => void
}) {
  return (
    <div className="grid gap-2 md:hidden">
      {accounts.map((account) => (
        <Card key={account.id} className="min-w-0 w-full gap-3 rounded-lg py-4 shadow-none">
          <CardContent className="space-y-3 px-4">
            <div className="flex items-start justify-between gap-3">
              <AccountIdentity account={account} />
              <ScheduleSwitch account={account} busy={busyAccounts.has(account.id)} onToggle={onToggle} />
            </div>
            <GroupBadges account={account} />
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
              <MobileField label="状态"><StatusBadge status={account.status} /></MobileField>
              <MobileField label="来源"><ManagedBadge account={account} /></MobileField>
              <MobileField label="倍率"><span className="font-mono">{formatNumber(account.rate_multiplier)}</span></MobileField>
              <MobileField label="并发 / 优先级"><span className="font-mono">{account.concurrency} / {account.priority}</span></MobileField>
              <MobileField label="代理"><span>{account.proxy_name || "-"}</span></MobileField>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function AccountIdentity({ account }: { account: Sub2APIOverviewAccount }) {
  return (
    <div className="min-w-0">
      <p className="max-w-64 truncate font-medium text-foreground" title={account.name}>{account.name}</p>
      <p className="text-xs text-muted-foreground">#{account.id} · {account.platform || "未知平台"} · {account.type || "未知类型"}</p>
    </div>
  )
}

function GroupBadges({ account }: { account: Sub2APIOverviewAccount }) {
  if (!account.groups.length) return <Badge variant="outline">未分组</Badge>
  return (
    <div className="flex flex-wrap gap-1">
      {account.groups.map((item) => <Badge key={item.id} variant="secondary">{item.name}</Badge>)}
    </div>
  )
}

function ManagedBadge({ account }: { account: Sub2APIOverviewAccount }) {
  if (!account.managed) return <Badge variant="outline">非托管</Badge>
  const names = account.managed_sync_group_names?.join("、") || "UpstreamOps"
  return <Badge className="max-w-48 truncate bg-sky-100 text-sky-800 hover:bg-sky-100 dark:bg-sky-950 dark:text-sky-200" title={names}>{names}</Badge>
}

function ScheduleSwitch({
  account,
  busy,
  onToggle,
}: {
  account: Sub2APIOverviewAccount
  busy: boolean
  onToggle: (account: Sub2APIOverviewAccount, next: boolean) => void
}) {
  return (
    <div className="flex w-24 items-center gap-2">
      <Switch
        checked={account.schedulable}
        disabled={busy}
        aria-label={`${account.name}调度`}
        onCheckedChange={(next) => onToggle(account, next)}
      />
      <span className="text-xs text-muted-foreground">{busy ? "更新中" : account.schedulable ? "可调度" : "已停用"}</span>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const active = status.toLocaleLowerCase() === "active"
  return (
    <Badge
      variant="outline"
      className={cn(
        "border-transparent",
        active
          ? "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200"
          : "bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-200",
      )}
    >
      {statusLabel(status)}
    </Badge>
  )
}

function MobileField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1">
      <p className="text-muted-foreground">{label}</p>
      <div className="min-w-0 text-foreground">{children}</div>
    </div>
  )
}

function OverviewSkeleton() {
  return (
    <section className="space-y-5">
      <div className="flex items-center justify-between">
        <div className="space-y-2"><Skeleton className="h-6 w-40" /><Skeleton className="h-3 w-64" /></div>
        <Skeleton className="size-9" />
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        {Array.from({ length: 5 }, (_, index) => <Skeleton key={index} className="h-16" />)}
      </div>
      <Skeleton className="h-48 w-full" />
      <Skeleton className="h-80 w-full" />
    </section>
  )
}

function applySchedulableUpdate(data: Sub2APIOverview, accountID: number, schedulable: boolean): Sub2APIOverview {
  const accounts = data.accounts.map((account) => account.id === accountID ? { ...account, schedulable } : account)
  const groups = data.groups.map((group) => recalculateGroup(group, accounts))
  return {
    ...data,
    accounts,
    groups,
    summary: {
      ...data.summary,
      schedulable_accounts: accounts.filter((account) => account.schedulable).length,
    },
  }
}

function recalculateGroup(group: Sub2APIOverviewGroup, accounts: Sub2APIOverviewAccount[]): Sub2APIOverviewGroup {
  const members = accounts.filter((account) =>
    group.id === 0 ? account.groups.length === 0 : account.groups.some((item) => item.id === group.id),
  )
  return {
    ...group,
    account_count: members.length,
    active_account_count: members.filter((account) => account.status.toLocaleLowerCase() === "active").length,
    schedulable_account_count: members.filter((account) => account.schedulable).length,
  }
}

function uniqueSorted(items: string[]) {
  return [...new Set(items)].sort((a, b) => a.localeCompare(b))
}

function statusLabel(status: string) {
  const normalized = status.toLocaleLowerCase()
  if (normalized === "active") return "启用"
  if (normalized === "inactive" || normalized === "disabled") return "禁用"
  return status || "未知"
}

function formatNumber(value: number) {
  return Number.isFinite(value) ? value.toLocaleString("zh-CN", { maximumFractionDigits: 6 }) : "-"
}
