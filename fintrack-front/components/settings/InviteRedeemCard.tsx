import React, { useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import { authAPI } from '../../services/apiService';

interface InviteRedeemCardProps {
  isDemoMode?: boolean;
}

const InviteRedeemCard: React.FC<InviteRedeemCardProps> = ({ isDemoMode = false }) => {
  const { t } = useLanguage();
  const [inviteCode, setInviteCode] = useState('');
  const [redeemingInvite, setRedeemingInvite] = useState(false);
  const [inviteMessage, setInviteMessage] = useState('');
  const [inviteError, setInviteError] = useState('');

  const handleRedeemInvite = async () => {
    const code = inviteCode.trim();
    if (!code) {
      setInviteError(t('settings.inviteCodeRequired'));
      return;
    }
    setRedeemingInvite(true);
    setInviteMessage('');
    setInviteError('');
    try {
      const response = await authAPI.redeemInvite(code);
      const expiresAt = new Date(response.membership_expires_at).toLocaleDateString();
      setInviteCode('');
      setInviteMessage(t('settings.inviteRedeemSuccess')
        .replace('{level}', String(response.membership_level))
        .replace('{date}', expiresAt));
    } catch (error: any) {
      setInviteError(error?.message || t('settings.inviteRedeemFailed'));
    } finally {
      setRedeemingInvite(false);
    }
  };

  return (
    <div className="rounded-2xl border border-white/10 bg-card-dark p-5">
      <div className="mb-4 flex items-center gap-3">
        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl bg-primary/12 text-primary">
          <span className="material-symbols-outlined">redeem</span>
        </div>
        <div>
          <h2 className="text-white text-lg font-bold leading-normal">{t('settings.inviteTitle')}</h2>
          <p className="text-white/50 text-sm leading-normal">{t('settings.inviteDesc')}</p>
        </div>
      </div>
      <div className="flex flex-col gap-3">
        <input
          value={inviteCode}
          onChange={(event) => setInviteCode(event.target.value)}
          placeholder="VIP..."
          disabled={isDemoMode || redeemingInvite}
          className="h-11 rounded-xl border border-white/10 bg-black/20 px-4 text-sm font-semibold uppercase text-white outline-none transition-colors placeholder:text-white/25 focus:border-primary/60 disabled:opacity-50"
        />
        <button
          type="button"
          onClick={handleRedeemInvite}
          disabled={isDemoMode || redeemingInvite}
          className="flex h-11 w-full items-center justify-center gap-2 rounded-xl bg-primary px-4 text-sm font-black text-background-dark transition-colors hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <span className="material-symbols-outlined text-[18px]">workspace_premium</span>
          <span>{redeemingInvite ? t('settings.inviteRedeeming') : t('settings.inviteRedeem')}</span>
        </button>
        <div className="min-h-5 text-sm">
          {inviteError && <span className="text-red-200">{inviteError}</span>}
          {!inviteError && inviteMessage && <span className="text-emerald-200">{inviteMessage}</span>}
        </div>
      </div>
    </div>
  );
};

export default InviteRedeemCard;
