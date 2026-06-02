import React from 'react';
import MTFAgentPanel from './MTFAgentPanel';

interface MTFAgentPageProps {
    onAuthError?: () => void;
    onOpenSettings?: () => void;
}

const MTFAgentPage: React.FC<MTFAgentPageProps> = ({ onAuthError, onOpenSettings }) => {
    return (
        <div className="flex h-[calc(100vh-6rem)] min-h-[640px] flex-col overflow-hidden rounded-2xl border border-white/10 bg-[#101417] shadow-[0_24px_70px_rgba(0,0,0,0.24)] lg:h-[calc(100vh-4rem)]">
            <MTFAgentPanel
                onAuthError={onAuthError}
                onOpenSettings={onOpenSettings}
                className="h-full"
            />
        </div>
    );
};

export default MTFAgentPage;
