import React from 'react';
import { AdminGatewayQueueStatus } from '../../services/apiService';

interface GatewayQueuePanelProps {
  status: AdminGatewayQueueStatus | null;
  isLoading: boolean;
  onRefresh: () => void;
  copy: {
    title: string;
    subtitle: string;
    refresh: string;
    loading: string;
    queueDepth: string;
    queued: string;
    running: string;
    succeeded: string;
    failed: string;
    backends: string;
    backend: string;
    role: string;
    capacity: string;
    inFlight: string;
    available: string;
    capabilities: string;
    reachable: string;
    unreachable: string;
    lastUpdated: string;
    noBackends: string;
  };
}

const jobCount = (status: AdminGatewayQueueStatus | null, key: string) => status?.jobs?.[key] ?? 0;

const GatewayQueuePanel: React.FC<GatewayQueuePanelProps> = ({ status, isLoading, onRefresh, copy }) => {
  const isReachable = Boolean(status?.reachable);
  const backends = status?.backends || [];
  const updatedAt = status?.timestamp
    ? new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(status.timestamp))
    : '-';

  return (
    <section className="rounded-2xl border border-white/10 bg-card-dark p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="material-symbols-outlined text-[20px] text-primary">hub</span>
            <h2 className="text-lg font-bold text-white">{copy.title}</h2>
            <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${isReachable ? 'bg-emerald-400/10 text-emerald-200' : 'bg-red-400/10 text-red-100'}`}>
              {isReachable ? copy.reachable : copy.unreachable}
            </span>
          </div>
          <p className="mt-1 text-sm text-white/45">{copy.subtitle}</p>
        </div>
        <button
          type="button"
          onClick={onRefresh}
          disabled={isLoading}
          className="inline-flex h-10 items-center gap-2 rounded-xl border border-white/10 bg-white/[0.04] px-3 text-sm font-bold text-white/72 transition-colors hover:bg-white/10 hover:text-white disabled:cursor-not-allowed disabled:opacity-55"
        >
          <span className={`material-symbols-outlined text-[18px] ${isLoading ? 'animate-spin' : ''}`}>refresh</span>
          {copy.refresh}
        </button>
      </div>

      {status?.error && (
        <div className="mt-4 rounded-xl border border-red-400/20 bg-red-500/10 px-4 py-3 text-sm text-red-100/85">
          {status.error}
        </div>
      )}

      <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label={copy.queueDepth} value={status?.queue_depth ?? 0} tone="text-primary" />
        <Metric label={copy.queued} value={jobCount(status, 'queued')} tone="text-yellow-200" />
        <Metric label={copy.running} value={jobCount(status, 'running')} tone="text-sky-200" />
        <Metric label={copy.succeeded} value={jobCount(status, 'succeeded')} tone="text-emerald-200" />
        <Metric label={copy.failed} value={jobCount(status, 'failed')} tone="text-red-200" />
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-white/40">
        <span>{copy.lastUpdated}: {isLoading ? copy.loading : updatedAt}</span>
        {status?.source_url && <span className="max-w-full truncate font-mono">{status.source_url}</span>}
      </div>

      <div className="mt-5 overflow-x-auto">
        <table className="w-full min-w-[760px] text-left text-sm">
          <thead className="text-white/40">
            <tr>
              <th className="py-2">{copy.backend}</th>
              <th>{copy.role}</th>
              <th>{copy.capacity}</th>
              <th>{copy.inFlight}</th>
              <th>{copy.available}</th>
              <th>{copy.capabilities}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/8 text-white/78">
            {backends.map((backend) => (
              <tr key={`${backend.name}-${backend.url}`}>
                <td className="py-3">
                  <div className="font-bold text-white">{backend.name}</div>
                  <div className="mt-1 max-w-[280px] truncate font-mono text-xs text-white/35">{backend.url}</div>
                </td>
                <td>{backend.role || '-'}</td>
                <td>{backend.capacity}</td>
                <td>{backend.in_flight}</td>
                <td>{backend.available}</td>
                <td>
                  <div className="flex flex-wrap gap-1.5">
                    {backend.supports_mtf_pro && <Badge>MTF-PRO</Badge>}
                    {backend.supports_direct_cov && <Badge>DIRECT</Badge>}
                    {backend.supports_mtf_lite && <Badge>MTF-LITE</Badge>}
                    {backend.supports_uzi && <Badge>UZI</Badge>}
                  </div>
                </td>
              </tr>
            ))}
            {backends.length === 0 && (
              <tr>
                <td className="py-4 text-white/45" colSpan={6}>{copy.noBackends}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
};

const Metric: React.FC<{ label: string; value: number; tone: string }> = ({ label, value, tone }) => (
  <div className="rounded-xl border border-white/10 bg-black/20 p-4">
    <p className="text-xs font-bold text-white/40">{label}</p>
    <p className={`mt-2 text-2xl font-black ${tone}`}>{value}</p>
  </div>
);

const Badge: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <span className="rounded-full border border-white/10 bg-white/[0.04] px-2 py-0.5 text-[11px] font-bold text-white/65">
    {children}
  </span>
);

export default GatewayQueuePanel;
