import type { PredictionChartDetailMode } from './PredictionChart';

export const PREDICTION_CHART_TEXT = {
    axisFontSize: 12,
    axisSecondLineOffset: 13,
    mobileAxisFontSize: 11,
    mobileAxisSecondLineOffset: 12,
    cornerFontSize: 13,
    mobileCornerFontSize: 10.5,
    emptyFontSize: 6,
    tradeLabelFontSize: 8,
    deltaLabelFontSize: 12,
    mobileDeltaLabelFontSize: 11,
    deltaPaddingX: 6,
    deltaPaddingY: 2.5,
    tooltipFontSize: 12,
    tooltipMetaFontSize: 11,
};

export const getChartTextScale = (
    detailMode: PredictionChartDetailMode,
    width: number,
    height: number,
) => (
    detailMode === 'expanded'
        ? Math.min(2.4, Math.max(1, Math.min(width / 478, height / 150)))
        : 1
);

export const getChartAxisTextScale = (
    detailMode: PredictionChartDetailMode,
    width: number,
    height: number,
) => (
    detailMode === 'expanded'
        ? Math.min(1.65, Math.max(1, Math.min(width / 520, height / 190)))
        : 1
);
