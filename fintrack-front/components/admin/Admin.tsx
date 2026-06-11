import React, { useEffect, useMemo, useState } from 'react';
import { StrategyParams } from '../../types';
import { adminAPI, AdminGatewayQueueStatus, CreateMembershipInviteRequest, MembershipInviteCode } from '../../services/apiService';
import { useLanguage } from '../../contexts/LanguageContext';
import GatewayQueuePanel from './GatewayQueuePanel';

const createDefaultStrategy = (name: string): StrategyParams => ({
  unique_key: 'strategy_custom_system',
  name,
  is_public: 1,
  buy_threshold_pct: 1.5,
  sell_threshold_pct: -1.2,
  initial_cash: 10000,
  enable_rebalance: true,
  max_position_pct: 0.75,
  min_position_pct: 0.15,
  slope_position_per_pct: 0.12,
  rebalance_tolerance_pct: 0.06,
  trade_fee_rate: 0.006,
  take_profit_threshold_pct: 12,
  take_profit_sell_frac: 0.5,
});

const inputControlClass = 'h-11 rounded-xl border border-white/10 bg-black/20 px-3 text-sm text-white outline-none transition-colors placeholder:text-white/35 focus:border-primary focus:bg-black/30';
const selectControlClass = `${inputControlClass} w-full appearance-none pr-10`;
const checkboxLabelClass = 'flex h-11 cursor-pointer items-center gap-2 rounded-xl border border-white/10 bg-black/20 px-3 text-sm text-white/80 transition-colors hover:border-white/20 hover:bg-white/[0.04]';
const checkboxBoxClass = 'flex h-5 w-5 items-center justify-center rounded-md border border-white/15 bg-black/30 text-transparent transition-colors peer-checked:border-primary peer-checked:bg-primary peer-checked:text-background-dark peer-focus-visible:ring-2 peer-focus-visible:ring-primary/50';

const Admin: React.FC = () => {
  const { language } = useLanguage();
  const isZh = language === 'zh';
  const copy = {
    systemStrategyName: isZh ? '系统策略' : 'System Strategy',
    checking: isZh ? '正在校验管理员权限...' : 'Checking admin access...',
    checkFailed: isZh ? '管理员校验失败' : 'Admin check failed',
    noAccess: isZh ? '无管理员权限' : 'No Admin Access',
    noAccessDesc: isZh ? '当前账号不能访问管理员页面。' : 'The current account cannot access the admin page.',
    title: isZh ? '管理员' : 'Admin',
    subtitle: isZh ? '管理会员邀请码和系统回测策略。' : 'Manage membership invite codes and system backtest strategies.',
    totalInvites: isZh ? '邀请码总数' : 'Total Invites',
    active: isZh ? '启用中' : 'Active',
    systemStrategies: isZh ? '系统策略' : 'System Strategies',
    gatewayTitle: isZh ? 'Gateway 队列' : 'Gateway Queue',
    gatewaySubtitle: isZh ? '推理网关当前队列、任务状态与后端容量。' : 'Current inference gateway queue, job state, and backend capacity.',
    refresh: isZh ? '刷新' : 'Refresh',
    loading: isZh ? '加载中' : 'Loading',
    queueDepth: isZh ? '队列深度' : 'Queue Depth',
    queued: isZh ? '排队中' : 'Queued',
    running: isZh ? '运行中' : 'Running',
    succeeded: isZh ? '已完成' : 'Succeeded',
    failed: isZh ? '失败' : 'Failed',
    backends: isZh ? '后端' : 'Backends',
    backend: isZh ? '后端' : 'Backend',
    role: isZh ? '角色' : 'Role',
    capacity: isZh ? '容量' : 'Capacity',
    inFlight: isZh ? '执行中' : 'In Flight',
    available: isZh ? '可用' : 'Available',
    capabilities: isZh ? '能力' : 'Capabilities',
    reachable: isZh ? '可达' : 'Reachable',
    unreachable: isZh ? '不可达' : 'Unreachable',
    lastUpdated: isZh ? '更新时间' : 'Updated',
    noBackends: isZh ? '暂无后端数据' : 'No backend data',
    addInvite: isZh ? '添加邀请码' : 'Add Invite Code',
    autoGenerate: isZh ? '留空自动生成' : 'Leave empty to auto-generate',
    days: isZh ? '天' : 'days',
    maxUses: isZh ? '使用次数' : 'Max Uses',
    enabled: isZh ? '启用' : 'Enabled',
    disabled: isZh ? '停用' : 'Disabled',
    note: isZh ? '备注' : 'Note',
    creating: isZh ? '创建中...' : 'Creating...',
    createInvite: isZh ? '创建邀请码' : 'Create Invite Code',
    createInviteFailed: isZh ? '创建邀请码失败' : 'Failed to create invite code',
    code: isZh ? '邀请码' : 'Code',
    level: isZh ? '等级' : 'Level',
    dayCount: isZh ? '天数' : 'Days',
    used: isZh ? '使用次数' : 'Usage',
    status: isZh ? '状态' : 'Status',
    copyInvite: isZh ? '复制邀请码' : 'Copy invite code',
    backtestStrategy: isZh ? '系统回测策略' : 'System Backtest Strategy',
    saveStrategyFailed: isZh ? '保存系统策略失败' : 'Failed to save system strategy',
    enableRebalance: isZh ? '启用动态调仓' : 'Enable Dynamic Rebalance',
    saving: isZh ? '保存中...' : 'Saving...',
    saveStrategy: isZh ? '保存系统策略' : 'Save System Strategy',
  };
  const defaultStrategy = useMemo(() => createDefaultStrategy(copy.systemStrategyName), [copy.systemStrategyName]);
  const strategyFields = useMemo<Array<{ key: keyof StrategyParams; label: string; step?: string }>>(() => [
    { key: 'buy_threshold_pct', label: isZh ? '买入阈值 %' : 'Buy Threshold %' },
    { key: 'sell_threshold_pct', label: isZh ? '卖出阈值 %' : 'Sell Threshold %' },
    { key: 'initial_cash', label: isZh ? '初始资金' : 'Initial Cash' },
    { key: 'max_position_pct', label: isZh ? '最大仓位' : 'Max Position' },
    { key: 'min_position_pct', label: isZh ? '最小仓位' : 'Min Position' },
    { key: 'slope_position_per_pct', label: isZh ? '斜率仓位系数' : 'Slope Position Factor' },
    { key: 'rebalance_tolerance_pct', label: isZh ? '调仓容差' : 'Rebalance Tolerance' },
    { key: 'trade_fee_rate', label: isZh ? '交易费率' : 'Trade Fee Rate', step: '0.0001' },
    { key: 'take_profit_threshold_pct', label: isZh ? '止盈阈值 %' : 'Take Profit Threshold %' },
    { key: 'take_profit_sell_frac', label: isZh ? '止盈卖出比例' : 'Take Profit Sell Fraction' },
  ], [isZh]);
  const [checking, setChecking] = useState(true);
  const [allowed, setAllowed] = useState(false);
  const [error, setError] = useState('');
  const [invites, setInvites] = useState<MembershipInviteCode[]>([]);
  const [strategies, setStrategies] = useState<StrategyParams[]>([]);
  const [gatewayQueue, setGatewayQueue] = useState<AdminGatewayQueueStatus | null>(null);
  const [loadingGatewayQueue, setLoadingGatewayQueue] = useState(false);
  const [inviteForm, setInviteForm] = useState<CreateMembershipInviteRequest>({
    code: '',
    membership_level: 1,
    duration_days: 30,
    max_uses: 50,
    is_active: true,
    note: '',
  });
  const [strategyForm, setStrategyForm] = useState<StrategyParams>(defaultStrategy);
  const [savingInvite, setSavingInvite] = useState(false);
  const [savingStrategy, setSavingStrategy] = useState(false);
  const [copiedInviteID, setCopiedInviteID] = useState<number | null>(null);

  const activeInviteCount = useMemo(() => invites.filter((item) => item.is_active).length, [invites]);

  const loadAdminData = async () => {
    const [inviteResult, strategyResult] = await Promise.all([
      adminAPI.listInviteCodes(),
      adminAPI.listSystemStrategies(),
    ]);
    setInvites(inviteResult.items || []);
    setStrategies(strategyResult.strategies || []);
    if ((strategyResult.strategies || []).length > 0) {
      setStrategyForm({ ...defaultStrategy, ...strategyResult.strategies[0] });
    }
  };

  const refreshGatewayQueue = async () => {
    setLoadingGatewayQueue(true);
    try {
      setGatewayQueue(await adminAPI.getGatewayQueue());
    } catch (err: any) {
      setGatewayQueue({
        reachable: false,
        status: 'unreachable',
        queue_depth: 0,
        jobs: {},
        backends: [],
        checked_path: '/health',
        error: err?.message || 'Failed to load gateway queue',
      });
    } finally {
      setLoadingGatewayQueue(false);
    }
  };

  useEffect(() => {
    let cancelled = false;
    const checkAdmin = async () => {
      setChecking(true);
      setError('');
      try {
        await adminAPI.getStatus();
        if (cancelled) return;
        setAllowed(true);
        await loadAdminData();
        if (!cancelled) {
          void refreshGatewayQueue();
        }
      } catch (err: any) {
        if (!cancelled) {
          setAllowed(false);
          setError(err?.message || copy.checkFailed);
        }
      } finally {
        if (!cancelled) setChecking(false);
      }
    };
    checkAdmin();
    return () => {
      cancelled = true;
    };
  }, []);

  const createInvite = async () => {
    setSavingInvite(true);
    setError('');
    try {
      const created = await adminAPI.createInviteCode({
        ...inviteForm,
        code: inviteForm.code?.trim() || undefined,
        note: inviteForm.note?.trim() || null,
      });
      setInvites((prev) => [created, ...prev]);
      setInviteForm({ code: '', membership_level: 1, duration_days: 30, max_uses: 50, is_active: true, note: '' });
    } catch (err: any) {
      setError(err?.message || copy.createInviteFailed);
    } finally {
      setSavingInvite(false);
    }
  };

  const toggleInvite = async (item: MembershipInviteCode) => {
    const updated = await adminAPI.setInviteCodeActive(item.id, !item.is_active);
    setInvites((prev) => prev.map((entry) => (entry.id === updated.id ? updated : entry)));
  };

  const copyInviteCode = async (item: MembershipInviteCode) => {
    await navigator.clipboard.writeText(item.code);
    setCopiedInviteID(item.id);
    window.setTimeout(() => {
      setCopiedInviteID((current) => (current === item.id ? null : current));
    }, 1600);
  };

  const saveStrategy = async () => {
    setSavingStrategy(true);
    setError('');
    try {
      const saved = await adminAPI.saveSystemStrategy({
        ...strategyForm,
        unique_key: strategyForm.unique_key.trim(),
        name: strategyForm.name?.trim() || copy.systemStrategyName,
        is_public: 1,
      });
      setStrategies((prev) => {
        const exists = prev.some((item) => item.unique_key === saved.unique_key);
        return exists ? prev.map((item) => (item.unique_key === saved.unique_key ? saved : item)) : [saved, ...prev];
      });
      setStrategyForm({ ...defaultStrategy, ...saved });
    } catch (err: any) {
      setError(err?.message || copy.saveStrategyFailed);
    } finally {
      setSavingStrategy(false);
    }
  };

  if (checking) {
    return <div className="p-8 text-white/70">{copy.checking}</div>;
  }

  if (!allowed) {
    return (
      <div className="rounded-2xl border border-red-400/20 bg-red-500/10 p-6 text-red-100">
        <h1 className="text-2xl font-black">{copy.noAccess}</h1>
        <p className="mt-2 text-sm text-red-100/75">{error || copy.noAccessDesc}</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-4xl font-black leading-tight text-white">{copy.title}</h1>
        <p className="text-sm text-white/55">{copy.subtitle}</p>
      </header>

      {error && <div className="rounded-xl border border-red-400/20 bg-red-500/10 px-4 py-3 text-sm text-red-100">{error}</div>}

      <section className="grid gap-4 lg:grid-cols-3">
        <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
          <p className="text-sm text-white/45">{copy.totalInvites}</p>
          <p className="mt-2 text-3xl font-black text-white">{invites.length}</p>
        </div>
        <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
          <p className="text-sm text-white/45">{copy.active}</p>
          <p className="mt-2 text-3xl font-black text-emerald-200">{activeInviteCount}</p>
        </div>
        <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
          <p className="text-sm text-white/45">{copy.systemStrategies}</p>
          <p className="mt-2 text-3xl font-black text-primary">{strategies.length}</p>
        </div>
      </section>

      <GatewayQueuePanel
        status={gatewayQueue}
        isLoading={loadingGatewayQueue}
        onRefresh={refreshGatewayQueue}
        copy={{
          title: copy.gatewayTitle,
          subtitle: copy.gatewaySubtitle,
          refresh: copy.refresh,
          loading: copy.loading,
          queueDepth: copy.queueDepth,
          queued: copy.queued,
          running: copy.running,
          succeeded: copy.succeeded,
          failed: copy.failed,
          backends: copy.backends,
          backend: copy.backend,
          role: copy.role,
          capacity: copy.capacity,
          inFlight: copy.inFlight,
          available: copy.available,
          capabilities: copy.capabilities,
          reachable: copy.reachable,
          unreachable: copy.unreachable,
          lastUpdated: copy.lastUpdated,
          noBackends: copy.noBackends,
        }}
      />

      <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
          <h2 className="text-lg font-bold text-white">{copy.addInvite}</h2>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            <input className={`${inputControlClass} uppercase`} placeholder={copy.autoGenerate} value={inviteForm.code || ''} onChange={(e) => setInviteForm({ ...inviteForm, code: e.target.value })} />
            <div className="relative">
              <select className={selectControlClass} value={inviteForm.membership_level} onChange={(e) => setInviteForm({ ...inviteForm, membership_level: Number(e.target.value) })}>
                <option value={1}>VIP</option>
              </select>
              <span className="material-symbols-outlined pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[20px] text-white/45">expand_more</span>
            </div>
            <div className="relative">
              <input className={`${inputControlClass} w-full pr-10`} type="number" min={1} value={inviteForm.duration_days} onChange={(e) => setInviteForm({ ...inviteForm, duration_days: Number(e.target.value) })} />
              <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-bold text-white/45">{copy.days}</span>
            </div>
            <div className="relative">
              <input className={`${inputControlClass} w-full pr-20`} type="number" min={1} value={inviteForm.max_uses ?? 50} onChange={(e) => setInviteForm({ ...inviteForm, max_uses: Number(e.target.value) })} />
              <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-bold text-white/45">{copy.maxUses}</span>
            </div>
            <label className={checkboxLabelClass}>
              <input className="peer sr-only" type="checkbox" checked={Boolean(inviteForm.is_active)} onChange={(e) => setInviteForm({ ...inviteForm, is_active: e.target.checked })} />
              <span className={checkboxBoxClass}>
                <span className="material-symbols-outlined text-[16px]">check</span>
              </span>
              {copy.enabled}
            </label>
            <input className={`${inputControlClass} sm:col-span-2`} placeholder={copy.note} value={inviteForm.note || ''} onChange={(e) => setInviteForm({ ...inviteForm, note: e.target.value })} />
          </div>
          <button onClick={createInvite} disabled={savingInvite} className="mt-4 inline-flex h-11 items-center gap-2 rounded-xl bg-primary px-5 text-sm font-black text-background-dark disabled:opacity-50">
            <span className="material-symbols-outlined text-[18px]">add</span>
            {savingInvite ? copy.creating : copy.createInvite}
          </button>

          <div className="mt-5 overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-sm">
              <thead className="text-white/40">
                <tr><th className="py-2">{copy.code}</th><th>{copy.level}</th><th>{copy.dayCount}</th><th>{copy.used}</th><th>{copy.status}</th><th></th></tr>
              </thead>
              <tbody className="divide-y divide-white/8 text-white/78">
                {invites.map((item) => (
                  <tr key={item.id}>
                    <td className="py-3">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-white">{item.code}</span>
                        <button type="button" title={copy.copyInvite} onClick={() => copyInviteCode(item)} className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-white/45 transition-colors hover:bg-white/10 hover:text-white">
                          <span className="material-symbols-outlined text-[16px]">{copiedInviteID === item.id ? 'done' : 'content_copy'}</span>
                        </button>
                      </div>
                    </td>
                    <td>{item.membership_level}</td>
                    <td>{item.duration_days}</td>
                    <td>{item.used_count}/{item.max_uses}</td>
                    <td>{item.is_active ? copy.enabled : copy.disabled}</td>
                    <td><button className="rounded-lg border border-white/10 px-3 py-1.5 text-xs hover:bg-white/8" onClick={() => toggleInvite(item)}>{item.is_active ? copy.disabled : copy.enabled}</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
          <h2 className="text-lg font-bold text-white">{copy.backtestStrategy}</h2>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            <div className="relative sm:col-span-2">
              <select className={selectControlClass} value={strategyForm.unique_key} onChange={(e) => setStrategyForm({ ...defaultStrategy, ...strategies.find((item) => item.unique_key === e.target.value) })}>
                {strategies.map((item) => <option key={item.unique_key} value={item.unique_key}>{item.name || item.unique_key}</option>)}
                {!strategies.some((item) => item.unique_key === strategyForm.unique_key) && <option value={strategyForm.unique_key}>{strategyForm.unique_key}</option>}
              </select>
              <span className="material-symbols-outlined pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[20px] text-white/45">expand_more</span>
            </div>
            <input className={inputControlClass} value={strategyForm.unique_key} onChange={(e) => setStrategyForm({ ...strategyForm, unique_key: e.target.value })} />
            <input className={inputControlClass} value={strategyForm.name || ''} onChange={(e) => setStrategyForm({ ...strategyForm, name: e.target.value })} />
            {strategyFields.map((field) => (
              <label key={field.key} className="flex flex-col gap-1">
                <span className="text-xs font-bold text-white/40">{field.label}</span>
                <input className={`${inputControlClass} h-10`} type="number" step={field.step || '0.01'} value={Number(strategyForm[field.key] || 0)} onChange={(e) => setStrategyForm({ ...strategyForm, [field.key]: Number(e.target.value) })} />
              </label>
            ))}
            <label className={`${checkboxLabelClass} h-10`}>
              <input className="peer sr-only" type="checkbox" checked={strategyForm.enable_rebalance} onChange={(e) => setStrategyForm({ ...strategyForm, enable_rebalance: e.target.checked })} />
              <span className={checkboxBoxClass}>
                <span className="material-symbols-outlined text-[16px]">check</span>
              </span>
              {copy.enableRebalance}
            </label>
          </div>
          <button onClick={saveStrategy} disabled={savingStrategy} className="mt-4 inline-flex h-11 items-center gap-2 rounded-xl bg-primary px-5 text-sm font-black text-background-dark disabled:opacity-50">
            <span className="material-symbols-outlined text-[18px]">save</span>
            {savingStrategy ? copy.saving : copy.saveStrategy}
          </button>
        </div>
      </section>
    </div>
  );
};

export default Admin;
