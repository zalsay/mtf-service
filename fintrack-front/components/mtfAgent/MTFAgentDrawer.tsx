import React, { useEffect, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import MTFAgentPanel from './MTFAgentPanel';

interface MTFAgentDrawerProps {
    onAuthError?: () => void;
    onOpenSettings?: () => void;
}

const AGENT_GOLD_GRADIENT = 'linear-gradient(135deg, #FFF1B8 0%, #FCD34D 34%, #F59E0B 66%, #F97316 100%)';
const DEFAULT_DRAWER_WIDTH = 520;
const MIN_DRAWER_WIDTH = 360;
const MAX_DRAWER_WIDTH_RATIO = 0.5;
const RESIZABLE_VIEWPORT_WIDTH = 768;

const getMaxDrawerWidth = (viewportWidth: number) => Math.max(MIN_DRAWER_WIDTH, Math.floor(viewportWidth * MAX_DRAWER_WIDTH_RATIO));

const clampDrawerWidth = (width: number, viewportWidth: number) => Math.min(Math.max(width, MIN_DRAWER_WIDTH), getMaxDrawerWidth(viewportWidth));

const MTFAgentDrawer: React.FC<MTFAgentDrawerProps> = ({ onAuthError, onOpenSettings }) => {
    const { t } = useLanguage();
    const [open, setOpen] = useState(false);
    const [drawerWidth, setDrawerWidth] = useState(DEFAULT_DRAWER_WIDTH);
    const [resizing, setResizing] = useState(false);
    const [isResizableViewport, setIsResizableViewport] = useState(false);

    useEffect(() => {
        if (!open) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') setOpen(false);
        };
        window.addEventListener('keydown', onKeyDown);
        return () => window.removeEventListener('keydown', onKeyDown);
    }, [open]);

    useEffect(() => {
        if (!open) return;

        const handleResize = () => {
            const canResize = window.innerWidth >= RESIZABLE_VIEWPORT_WIDTH;
            setIsResizableViewport(canResize);
            if (canResize) {
                setDrawerWidth(width => clampDrawerWidth(width, window.innerWidth));
            }
        };

        handleResize();
        window.addEventListener('resize', handleResize);
        return () => window.removeEventListener('resize', handleResize);
    }, [open]);

    useEffect(() => {
        if (!resizing) return;

        const handlePointerMove = (event: PointerEvent) => {
            event.preventDefault();
            setDrawerWidth(clampDrawerWidth(window.innerWidth - event.clientX, window.innerWidth));
        };
        const handlePointerUp = () => {
            setResizing(false);
        };

        const previousCursor = document.body.style.cursor;
        const previousUserSelect = document.body.style.userSelect;

        document.body.style.cursor = 'ew-resize';
        document.body.style.userSelect = 'none';
        window.addEventListener('pointermove', handlePointerMove);
        window.addEventListener('pointerup', handlePointerUp);
        window.addEventListener('pointercancel', handlePointerUp);

        return () => {
            document.body.style.cursor = previousCursor;
            document.body.style.userSelect = previousUserSelect;
            window.removeEventListener('pointermove', handlePointerMove);
            window.removeEventListener('pointerup', handlePointerUp);
            window.removeEventListener('pointercancel', handlePointerUp);
        };
    }, [resizing]);

    return (
        <>
            <button
                type="button"
                aria-label={t('mtfAgent.open')}
                title={t('mtfAgent.title')}
                onClick={() => setOpen(true)}
                className="fixed bottom-20 right-4 z-40 flex h-14 w-14 items-center justify-center rounded-full border border-amber-200/45 text-[#18130b] shadow-[0_16px_42px_rgba(0,0,0,0.34)] transition-opacity hover:opacity-95 focus:outline-none focus:ring-2 focus:ring-amber-200/70 lg:bottom-6 lg:right-6"
                style={{ background: AGENT_GOLD_GRADIENT }}
            >
                <span className="material-symbols-outlined text-[27px]" style={{ fontVariationSettings: "'FILL' 1" }}>smart_toy</span>
            </button>

            {open && (
                <div className="fixed inset-0 z-[90]">
                    <button
                        type="button"
                        aria-label={t('mtfAgent.close')}
                        onClick={() => setOpen(false)}
                        className="absolute inset-0 bg-black/55 backdrop-blur-[2px]"
                    />
                    <aside
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby="mtf-agent-title"
                        className="absolute bottom-0 right-0 top-0 flex w-full max-w-[520px] flex-col border-l border-white/10 bg-[#101417] shadow-2xl"
                        style={isResizableViewport ? { width: drawerWidth, maxWidth: '50vw' } : undefined}
                    >
                        <button
                            type="button"
                            aria-label={t('mtfAgent.resize')}
                            title={t('mtfAgent.dragResize')}
                            onPointerDown={(event) => {
                                event.preventDefault();
                                event.stopPropagation();
                                if (!isResizableViewport) return;
                                setResizing(true);
                            }}
                            onDoubleClick={() => setDrawerWidth(clampDrawerWidth(DEFAULT_DRAWER_WIDTH, window.innerWidth))}
                            className={`absolute -left-2 top-0 hidden h-full w-4 cursor-ew-resize touch-none items-center justify-center md:flex ${resizing ? 'bg-amber-200/10' : 'bg-transparent'}`}
                        >
                            <span className="h-16 w-1 rounded-full bg-white/20 transition-colors hover:bg-amber-200/70" />
                        </button>
                        <MTFAgentPanel
                            onAuthError={onAuthError}
                            onOpenSettings={onOpenSettings}
                            onClose={() => setOpen(false)}
                            className="h-full"
                        />
                    </aside>
                </div>
            )}
        </>
    );
};

export default MTFAgentDrawer;
