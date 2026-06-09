import React, { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useLanguage } from '../../contexts/LanguageContext';
import type { PredictionChartData } from '../../types';
import { getChartColor } from '../../utils/colorUtils';
import { getChartTextScale, PREDICTION_CHART_TEXT } from './chartTextStyles';

export type PredictionChartMode = 'price' | 'change';
export type PredictionChartTheme = 'default' | 'lite' | 'pro';
export type PredictionChartDetailMode = 'normal' | 'expanded';

export interface PredictionChartMarker {
    index: number;
    type: 'buy' | 'sell';
    price?: number;
    date?: string;
    reason?: string;
}

interface ChartProps {
    change: number;
    chartData?: PredictionChartData;
    currentPrice?: number;
    startPrice?: number;
    mode?: PredictionChartMode;
    theme?: PredictionChartTheme;
    markers?: PredictionChartMarker[];
    detailMode?: PredictionChartDetailMode;
    fitToContainer?: boolean;
    actualLabel?: string;
}

const PredictionChart: React.FC<ChartProps> = ({
    change,
    chartData,
    currentPrice,
    startPrice,
    mode = 'price',
    theme = 'default',
    markers = [],
    detailMode = 'normal',
    fitToContainer = false,
    actualLabel: actualLabelOverride,
}) => {
    const { language, t } = useLanguage();
    const isPositive = change >= 0;
    const color = getChartColor(isPositive, language);
    const actualValueColor = isPositive ? '#EF4444' : '#22C55E';
    const [hoverIndex, setHoverIndex] = useState<number | null>(null);
    const [isMobileViewport, setIsMobileViewport] = useState(false);
    const containerRef = useRef<HTMLDivElement>(null);
    const tooltipRef = useRef<HTMLDivElement>(null);
    const gradientSeed = useId().replace(/:/g, '');
    const actualChangeGradientId = `paint-change-${gradientSeed}`;
    const predictionFillGradientId = `paint-prediction-fill-${gradientSeed}`;
    const proGradientId = `paint-pro-${gradientSeed}`;
    const proAccentColor = '#FBBF24';
    const litePredictionColor = '#F8FAFC';
    const [tooltipLeft, setTooltipLeft] = useState<number>(8);
    const [measuredFrame, setMeasuredFrame] = useState({ width: 478, height: 150 });
    const tooltipPointCount = chartData
        ? Math.max(
            chartData.actuals?.length || 0,
            chartData.predictions?.length || 0,
            chartData.proPredictions?.length || 0,
            chartData.actualChangePercents?.length || 0,
            chartData.predictedChangePercents?.length || 0,
            chartData.proPredictedChangePercents?.length || 0,
            chartData.dates?.length || 0,
        )
        : 1;

    useEffect(() => {
        if (typeof window === 'undefined') return;

        const mediaQuery = window.matchMedia('(max-width: 1023px)');
        const updateViewport = () => {
            setIsMobileViewport(mediaQuery.matches);
        };

        updateViewport();
        mediaQuery.addEventListener('change', updateViewport);

        return () => {
            mediaQuery.removeEventListener('change', updateViewport);
        };
    }, []);

    useLayoutEffect(() => {
        if ((!fitToContainer && detailMode !== 'expanded') || !containerRef.current || typeof ResizeObserver === 'undefined') {
            return;
        }

        const element = containerRef.current;
        const updateFrame = () => {
            const baseWidth = detailMode === 'expanded' ? 720 : 478;
            const baseHeight = detailMode === 'expanded' ? 220 : (isMobileViewport ? 162 : 150);
            const nextWidth = Math.max(baseWidth, Math.round(element.clientWidth || 0));
            const nextHeight = fitToContainer && detailMode !== 'expanded'
                ? baseHeight
                : Math.max(baseHeight, Math.round(element.clientHeight || 0));
            setMeasuredFrame(current => (
                current.width === nextWidth && current.height === nextHeight
                    ? current
                    : { width: nextWidth, height: nextHeight }
            ));
        };

        updateFrame();
        const observer = new ResizeObserver(updateFrame);
        observer.observe(element);

        return () => observer.disconnect();
    }, [detailMode, fitToContainer, isMobileViewport]);

    useLayoutEffect(() => {
        if (hoverIndex === null || !containerRef.current || !tooltipRef.current) {
            return;
        }

        const containerWidth = containerRef.current.clientWidth;
        const tooltipWidth = tooltipRef.current.offsetWidth;
        if (containerWidth <= 0 || tooltipWidth <= 0) {
            return;
        }

        const safePadding = 8;
        const anchorX = containerWidth * ((hoverIndex || 0) / Math.max(tooltipPointCount - 1, 1));
        const clampedLeft = Math.min(
            Math.max(anchorX - tooltipWidth / 2, safePadding),
            Math.max(safePadding, containerWidth - tooltipWidth - safePadding),
        );

        setTooltipLeft(clampedLeft);
    }, [hoverIndex, isMobileViewport, tooltipPointCount]);
    
    if (chartData && (
        chartData.actuals.length > 0
        || chartData.predictions.length > 0
        || (chartData.proPredictions?.length || 0) > 0
        || (chartData.actualChangePercents?.length || 0) > 0
        || (chartData.predictedChangePercents?.length || 0) > 0
        || (chartData.proPredictedChangePercents?.length || 0) > 0
    )) {
        const {
            actuals,
            predictions,
            dates,
            proPredictions = [],
            actualChangePercents = [],
            predictedChangePercents = [],
            proPredictedChangePercents = [],
        } = chartData;
        const isFinitePoint = (value: number | null | undefined): value is number => (
            value !== null && value !== undefined && Number.isFinite(Number(value))
        );
        const isNonZeroFinitePoint = (value: number | null | undefined): value is number => (
            isFinitePoint(value) && value !== 0
        );
        // Filter out 0 values to avoid "cliff-like" drops in the chart
        const allValues = [...actuals, ...predictions, ...proPredictions].filter(isNonZeroFinitePoint);
        const allChangeValues = [
            ...actualChangePercents,
            ...predictedChangePercents,
            ...proPredictedChangePercents,
        ].filter(isFinitePoint);
        const hasChangeSeries = allChangeValues.length > 0;
        const isChangeMode = mode === 'change';
        
        if (allValues.length > 0) {
            const min = Math.min(...allValues);
            const max = Math.max(...allValues);
            const range = max - min || 1;
            const changeMin = allChangeValues.length > 0 ? Math.min(...allChangeValues) : 0;
            const changeMax = allChangeValues.length > 0 ? Math.max(...allChangeValues) : 1;
            const changeRange = changeMax - changeMin || 1;
            const width = (detailMode === 'expanded' || fitToContainer) ? measuredFrame.width : 478;
            const height = (detailMode === 'expanded' || fitToContainer) ? measuredFrame.height : (isMobileViewport ? 162 : 150);
            const padding = detailMode === 'expanded' ? 36 : (isMobileViewport ? 32 : 20);
            const chartHeight = height - padding; // Reserve space for dates
            const labelScale = getChartTextScale(detailMode, width, height);
            const effectiveLabelScale = isMobileViewport ? 1 : labelScale;
            const axisFontBase = isMobileViewport
                ? PREDICTION_CHART_TEXT.mobileAxisFontSize
                : PREDICTION_CHART_TEXT.axisFontSize;
            const axisSecondLineBase = isMobileViewport
                ? PREDICTION_CHART_TEXT.mobileAxisSecondLineOffset
                : PREDICTION_CHART_TEXT.axisSecondLineOffset;
            const cornerFontBase = isMobileViewport
                ? PREDICTION_CHART_TEXT.mobileCornerFontSize
                : PREDICTION_CHART_TEXT.cornerFontSize;
            const axisLabelFontSize = axisFontBase;
            const axisSecondLineOffset = axisSecondLineBase;
            const predictionDeltaFontSize = isMobileViewport
                ? PREDICTION_CHART_TEXT.mobileDeltaLabelFontSize
                : PREDICTION_CHART_TEXT.deltaLabelFontSize;
            const predictionDeltaPaddingX = PREDICTION_CHART_TEXT.deltaPaddingX;
            const predictionDeltaPaddingY = PREDICTION_CHART_TEXT.deltaPaddingY;
            const cornerLabelFontSize = cornerFontBase;
            const emptyLabelFontSize = PREDICTION_CHART_TEXT.emptyFontSize * effectiveLabelScale;
            const tradeLabelFontSize = PREDICTION_CHART_TEXT.tradeLabelFontSize * effectiveLabelScale;
            const tooltipFontSize = PREDICTION_CHART_TEXT.tooltipFontSize;
            const tooltipMetaFontSize = PREDICTION_CHART_TEXT.tooltipMetaFontSize;
            
            // Assume both arrays align with the same X axis (dates)
            const count = Math.max(
                actuals.length,
                predictions.length,
                proPredictions.length,
                actualChangePercents.length,
                predictedChangePercents.length,
                proPredictedChangePercents.length,
                dates.length,
            );
            const stepX = count > 1 ? width / (count - 1) : 0;
            const getPriceY = (val: number) => chartHeight - ((val - min) / range) * (chartHeight - 20) - 10;
            const getChangeY = (val: number) => chartHeight - ((val - changeMin) / changeRange) * (chartHeight - 20) - 10;

            const generatePath = (data: number[], getYForValue: (val: number) => number, allowZero = false) => {
                if (!data || data.length === 0) return "";
                
                // Filter out zero values but keep index for X position calculation
                const validPointsData = data
                    .map((val, index) => ({ val, index }))
                    .filter(item => allowZero ? isFinitePoint(item.val) : isNonZeroFinitePoint(item.val));

                if (validPointsData.length === 0) return "";

                // Helper to get point coordinates
                const getPoint = (item: { val: number, index: number }) => {
                    const x = item.index * stepX;
                    const y = getYForValue(item.val);
                    return [x, y];
                };

                // Simple smoothing strategy (Catmull-Rom to Bezier)
                // For a point P[i], control points depends on P[i-1] and P[i+1]
                const smoothing = 0.2; // 0 to 1
                
                // Helper to calculate control point
                const line = (pointA: number[], pointB: number[]) => {
                    const lengthX = pointB[0] - pointA[0];
                    const lengthY = pointB[1] - pointA[1];
                    return {
                        length: Math.sqrt(Math.pow(lengthX, 2) + Math.pow(lengthY, 2)),
                        angle: Math.atan2(lengthY, lengthX)
                    };
                };

                const controlPoint = (current: number[], previous: number[], next: number[], reverse?: boolean) => {
                    const p = previous || current;
                    const n = next || current;
                    const o = line(p, n);
                    const angle = o.angle + (reverse ? Math.PI : 0);
                    const length = o.length * smoothing;
                    const x = current[0] + Math.cos(angle) * length;
                    const y = current[1] + Math.sin(angle) * length;
                    return [x, y];
                };

                const points = validPointsData.map(item => getPoint(item));
                
                return points.reduce((acc, point, i, a) => {
                    if (i === 0) return `M ${point[0]},${point[1]}`;
                    
                    const [cpsX, cpsY] = controlPoint(a[i - 1], a[i - 2], point);
                    const [cpeX, cpeY] = controlPoint(point, a[i - 1], a[i + 1], true);
                    
                    return `${acc} C ${cpsX},${cpsY} ${cpeX},${cpeY} ${point[0]},${point[1]}`;
                }, "");
            };

            const actualsPath = generatePath(actuals, getPriceY);
            const predictionsPath = generatePath(predictions, getPriceY);
            const proPredictionsPath = generatePath(proPredictions, getPriceY);
            const predictionFillPath = proPredictionsPath || predictionsPath;
            const predictionFillIsPro = Boolean(proPredictionsPath);
            const actualChangePath = generatePath(actualChangePercents, getChangeY, true);
            const predictedChangePath = generatePath(predictedChangePercents, getChangeY, true);
            const proPredictedChangePath = generatePath(proPredictedChangePercents, getChangeY, true);
            const actualStroke = actualValueColor;
            const actualDotStyle = { backgroundColor: actualStroke };
            const lastActualValue = [...actuals]
                .reverse()
                .find(value => isNonZeroFinitePoint(value));
            const formatPredictionDeltaLabel = (value: number) => {
                if (!lastActualValue || !isFinitePoint(value)) {
                    return null;
                }
                const delta = ((value - lastActualValue) / lastActualValue) * 100;
                if (!Number.isFinite(delta)) {
                    return null;
                }
                const isUp = delta >= 0;
                const prefix = isUp ? '+' : '';
                return {
                    text: `${isUp ? '▲' : '▼'} ${prefix}${delta.toFixed(2)}%`,
                    fill: isUp
                        ? (language === 'zh' ? '#DC2626' : '#16A34A')
                        : (language === 'zh' ? '#16A34A' : '#DC2626'),
                    background: isUp
                        ? (language === 'zh' ? '#FFF1F2' : '#ECFDF5')
                        : (language === 'zh' ? '#ECFDF5' : '#FFF1F2'),
                    border: isUp
                        ? (language === 'zh' ? '#FECACA' : '#BBF7D0')
                        : (language === 'zh' ? '#BBF7D0' : '#FECACA'),
                };
            };

            const formatDateLabel = (value: string) => {
                const normalizedValue = value.trim();
                const hyphenParts = normalizedValue.split('-');

                if (hyphenParts.length === 3 && hyphenParts.every(Boolean)) {
                    const [year, month, day] = hyphenParts;
                    return {
                        top: `${month}-${day}`,
                        bottom: year,
                    };
                }

                const slashParts = normalizedValue.split('/');
                if (slashParts.length === 3 && slashParts.every(Boolean)) {
                    const [year, month, day] = slashParts;
                    return {
                        top: `${month}/${day}`,
                        bottom: year,
                    };
                }

                return {
                    top: normalizedValue,
                    bottom: '',
                };
            };

            const formatTradeDateLabel = (value: string) => {
                const normalizedValue = value.trim();
                const compactMatch = normalizedValue.match(/^(\d{4})(\d{2})(\d{2})$/);
                if (compactMatch) {
                    return `${compactMatch[2]}/${compactMatch[3]}`;
                }

                const parts = normalizedValue.split(/[-/]/);
                if (parts.length === 3 && parts.every(Boolean)) {
                    return `${parts[1].padStart(2, '0')}/${parts[2].padStart(2, '0')}`;
                }

                return normalizedValue.slice(0, 5);
            };

            const estimateTradeLabelTextWidth = (text: string, fontSize: number) => (
                Array.from(text).reduce((total, char) => {
                    if (/\d/.test(char)) {
                        return total + fontSize * 0.56;
                    }
                    if (/[./,\-+:%]/.test(char)) {
                        return total + fontSize * 0.34;
                    }
                    if (/\s/.test(char)) {
                        return total + fontSize * 0.32;
                    }
                    if (/[\u4e00-\u9fff]/.test(char)) {
                        return total + fontSize;
                    }
                    return total + fontSize * 0.62;
                }, 0)
            );

            // Generate Date Labels (Start, Middle, End) -> (Start, 25%, 50%, 75%, End)
            const dateLabels: Array<{
                key: number;
                label: { top: string; bottom: string };
                leftPercent: number;
                text: string;
                translateXClass: string;
            }> = [];
            const labelCount = detailMode === 'expanded'
                ? Math.min(9, Math.max(5, Math.floor(width / 180)))
                : 5; // Target number of labels
            if (dates.length > 0) {
                 const step = Math.max(1, Math.floor((dates.length - 1) / (labelCount - 1)));
                 
                 for (let i = 0; i < labelCount; i++) {
                     let idx = i * step;
                     // Adjust last index to be exactly the end
                     if (i === labelCount - 1) idx = dates.length - 1;
                     // Prevent index out of bounds
                     if (idx >= dates.length) break;
                     
                     // Skip duplicates if any (e.g. short arrays)
                     if (i > 0 && idx === 0) continue;
                     
                     const x = idx * stepX;
                     const isFirst = i === 0;
                     const isLast = idx === dates.length - 1;
                     const label = formatDateLabel(dates[idx]);

                     dateLabels.push({
                        key: i,
                        label,
                        leftPercent: Math.min(Math.max((x / width) * 100, 0), 100),
                        text: dates[idx],
                        translateXClass: isFirst ? '' : isLast ? '-translate-x-full' : '-translate-x-1/2',
                     });
                 }
            }

            const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
                if (!containerRef.current || count <= 1) return;
                const rect = containerRef.current.getBoundingClientRect();
                const x = e.clientX - rect.left;
                const idx = Math.min(Math.max(0, Math.round((x / rect.width) * (count - 1))), count - 1);
                setHoverIndex(idx);
            };

            const handleMouseLeave = () => {
                setHoverIndex(null);
            };

            const actualLabel = actualLabelOverride || t('prediction.actual');
            const predLabel = t('prediction.pred');
            const proLabel = 'mtf-1.5-pro';
            const actualChangeLabel = language === 'zh' ? '实际涨跌' : 'Actual %';
            const predChangeLabel = language === 'zh' ? '预测涨跌' : 'Pred %';
            const getPredictionDeltaLabelLayout = (series: number[], value: number, index: number, laneOffset = 0) => {
                if (!isNonZeroFinitePoint(value) || isNonZeroFinitePoint(actuals[index])) {
                    return null;
                }

                const labelInfo = formatPredictionDeltaLabel(value);
                if (!labelInfo) {
                    return null;
                }

                const x = index * stepX;
                const pointY = getPriceY(value);
                const labelHeight = predictionDeltaFontSize + predictionDeltaPaddingY * 2 + 2;
                const labelGap = Math.max(7, 8 * labelScale);
                const visibleLabelOrder = series
                    .slice(0, index + 1)
                    .reduce((total, seriesValue, seriesIndex) => (
                        isNonZeroFinitePoint(seriesValue) && !isNonZeroFinitePoint(actuals[seriesIndex])
                            ? total + 1
                            : total
                    ), 0) - 1;
                let labelDirection = (visibleLabelOrder + laneOffset) % 2 === 0 ? -1 : 1;
                if (pointY < labelHeight + labelGap + 4) {
                    labelDirection = 1;
                } else if (pointY > chartHeight - labelHeight - labelGap - 4) {
                    labelDirection = -1;
                }
                const preferredY = labelDirection < 0
                    ? pointY - labelHeight - labelGap
                    : pointY + labelGap;
                const isNearLeft = x < 18 * labelScale;
                const isNearRight = x > width - 18 * labelScale;
                const labelY = Math.min(Math.max(preferredY, 4), Math.max(4, chartHeight - labelHeight - 2));
                const leftPercent = Math.min(Math.max((x / width) * 100, 1.5), 98.5);
                const topPercent = Math.min(Math.max((labelY / height) * 100, 2), 82);
                const translateXClass = isNearLeft ? '' : isNearRight ? '-translate-x-full' : '-translate-x-1/2';
                const labelAnchorX = (leftPercent / 100) * width;
                const labelAnchorY = (topPercent / 100) * height;
                const connectorEndY = labelDirection < 0
                    ? labelAnchorY + labelHeight
                    : labelAnchorY;

                return {
                    connectorEndY,
                    isNearRight,
                    labelAnchorX,
                    labelAnchorY,
                    labelInfo,
                    leftPercent,
                    pointX: x,
                    pointY,
                    topPercent,
                    translateXClass,
                };
            };

            const renderPredictionDeltaLabels = (
                series: number[],
                keyPrefix: string,
                laneOffset = 0,
            ) => series.map((value, index) => {
                const layout = getPredictionDeltaLabelLayout(series, value, index, laneOffset);
                if (!layout) {
                    return null;
                }

                return (
                    <div
                        key={`${keyPrefix}-${index}`}
                        className={`absolute z-[2] whitespace-nowrap rounded-lg border font-black leading-tight shadow-sm ${layout.translateXClass}`}
                        style={{
                            left: `${layout.leftPercent}%`,
                            top: `${layout.topPercent}%`,
                            color: layout.labelInfo.fill,
                            backgroundColor: layout.labelInfo.background,
                            borderColor: layout.labelInfo.border,
                            fontSize: `${predictionDeltaFontSize}px`,
                            padding: `${predictionDeltaPaddingY}px ${predictionDeltaPaddingX}px`,
                        }}
                    >
                        {layout.labelInfo.text}
                    </div>
                );
            });
            const renderPredictionDeltaConnectorLines = (
                series: number[],
                keyPrefix: string,
                laneOffset = 0,
            ) => series.map((value, index) => {
                const layout = getPredictionDeltaLabelLayout(series, value, index, laneOffset);
                if (!layout || !layout.isNearRight || index !== series.length - 1) {
                    return null;
                }

                return (
                    <line
                        key={`${keyPrefix}-connector-${index}`}
                        x1={layout.pointX}
                        y1={layout.pointY}
                        x2={layout.labelAnchorX - 18 * labelScale}
                        y2={layout.connectorEndY}
                        stroke={layout.labelInfo.border}
                        strokeLinecap="round"
                        strokeOpacity="0.95"
                        strokeWidth="1"
                        vectorEffect="non-scaling-stroke"
                    />
                );
            });
            const tradeLabelPaddingX = Math.max(4, 5 * labelScale);
            const tradeLabelPaddingY = Math.max(2, 3 * labelScale);
            const tradeLabelWidthBuffer = Math.max(6, 7 * labelScale);
            const tradeLabelLineHeight = tradeLabelFontSize * 1.15;
            const tradeMarkerLayouts = isChangeMode
                ? []
                : markers.map((marker, markerOrder) => {
                    const pointIndex = Math.min(Math.max(marker.index, 0), count - 1);
                    const value = actuals[pointIndex];
                    if (!isNonZeroFinitePoint(value)) {
                        return null;
                    }

                    const isBuy = marker.type === 'buy';
                    const tradePrice = Number.isFinite(marker.price) ? marker.price : value;
                    const label = Number.isFinite(tradePrice)
                        ? tradePrice.toLocaleString(language === 'zh' ? 'zh-CN' : 'en-US', {
                            minimumFractionDigits: 2,
                            maximumFractionDigits: 2,
                        })
                        : '--';
                    const dateLabel = formatTradeDateLabel(String(marker.date || dates[pointIndex] || ''));
                    const displayLabel = dateLabel ? `${dateLabel} ${label}` : label;
                    const markerColor = isBuy ? '#EF4444' : '#22C55E';
                    const estimatedTextWidth = estimateTradeLabelTextWidth(displayLabel, tradeLabelFontSize);
                    const labelWidth = Math.ceil(
                        tradeLabelPaddingX * 2 + estimatedTextWidth + tradeLabelWidthBuffer,
                    );
                    const labelHeight = Math.ceil(tradeLabelLineHeight + tradeLabelPaddingY * 2);
                    const x = pointIndex * stepX;
                    const y = getPriceY(value);
                    const stackOffset = (markerOrder % 3) * (labelHeight + 3 * labelScale);
                    const preferredY = isBuy
                        ? y - labelHeight - 9 * labelScale - stackOffset
                        : y + 11 * labelScale + stackOffset;
                    const labelY = Math.min(Math.max(preferredY, 3), chartHeight - labelHeight - 1);
                    const labelX = Math.min(Math.max(x - labelWidth / 2, 1), width - labelWidth - 1);
                    const connectorEndX = labelX + labelWidth / 2;
                    const connectorEndY = isBuy ? labelY + labelHeight : labelY;

                    return {
                        connectorEndX,
                        connectorEndY,
                        displayLabel,
                        isBuy,
                        labelHeight,
                        labelWidth,
                        labelX,
                        labelY,
                        markerColor,
                        pointIndex,
                        pointX: x,
                        pointY: y,
                    };
                }).filter((layout): layout is NonNullable<typeof layout> => layout !== null);

            const renderTradeMarkerAnchors = () => {
                if (tradeMarkerLayouts.length === 0) {
                    return null;
                }

                return tradeMarkerLayouts.map((layout, markerOrder) => (
                    <g key={`trade-marker-anchor-${layout.pointIndex}-${layout.isBuy ? 'buy' : 'sell'}-${markerOrder}`}>
                        <line
                            x1={layout.pointX}
                            y1={layout.pointY}
                            x2={layout.connectorEndX}
                            y2={layout.connectorEndY}
                            stroke={layout.markerColor}
                            strokeOpacity="0.68"
                            strokeWidth="0.8"
                            vectorEffect="non-scaling-stroke"
                        />
                        <circle
                            cx={layout.pointX}
                            cy={layout.pointY}
                            r={3 * labelScale}
                            fill={layout.markerColor}
                            stroke="#111827"
                            strokeWidth="1"
                        />
                    </g>
                ));
            };

            const renderTradeMarkerLabels = () => {
                if (tradeMarkerLayouts.length === 0) {
                    return null;
                }

                return tradeMarkerLayouts.map((layout, markerOrder) => {
                    const labelLeft = (layout.labelX / width) * 100;
                    const labelTop = (layout.labelY / height) * 100;

                    return (
                        <div
                            key={`trade-marker-label-${layout.pointIndex}-${layout.isBuy ? 'buy' : 'sell'}-${markerOrder}`}
                            className={`pointer-events-none absolute z-[3] flex items-center overflow-hidden rounded-md border font-extrabold leading-none shadow-sm ${
                                layout.isBuy
                                    ? 'border-red-300 bg-red-50 text-red-600'
                                    : 'border-green-300 bg-green-50 text-green-600'
                            }`}
                            style={{
                                height: `${layout.labelHeight}px`,
                                left: `${labelLeft}%`,
                                paddingLeft: `${tradeLabelPaddingX}px`,
                                paddingRight: `${tradeLabelPaddingX}px`,
                                top: `${labelTop}%`,
                                width: `${layout.labelWidth}px`,
                                fontSize: `${tradeLabelFontSize}px`,
                            }}
                        >
                            <span
                                className="min-w-0 flex-1 truncate"
                                title={layout.displayLabel}
                            >
                                {layout.displayLabel}
                            </span>
                        </div>
                    );
                });
            };

            const renderCornerPriceLabel = (value: number | undefined, side: 'left' | 'right') => {
                if (isChangeMode || value === undefined || !Number.isFinite(value)) {
                    return null;
                }

                return (
                    <div
                        className={`pointer-events-none absolute top-2 z-[2] rounded bg-black/30 px-2 py-1 font-semibold leading-none backdrop-blur-sm ${
                            side === 'left' ? 'left-2 text-white/70' : 'right-2 text-white'
                        }`}
                        style={{ fontSize: `${cornerLabelFontSize}px` }}
                    >
                        {value.toFixed(2)}
                    </div>
                );
            };

            return (
                <div ref={containerRef} className="relative w-full h-full" onMouseMove={handleMouseMove} onMouseLeave={handleMouseLeave}>
                    <svg fill="none" height="100%" preserveAspectRatio="none" viewBox={`-5 0 ${width + 12} ${height}`} width="100%" xmlns="http://www.w3.org/2000/svg">
                        {/* Area fill follows prediction curves; actuals remain line-only. */}
                        {!isChangeMode && predictionFillPath && (
                            <path d={`${predictionFillPath} V${chartHeight} H0 Z`} fill={`url(#${predictionFillGradientId})`} stroke="none"></path>
                        )}
                        {!isChangeMode && (
                            <>
                                {/* Actuals Line */}
                                <path d={actualsPath} stroke={actualStroke} strokeLinecap="round" strokeWidth={theme === 'pro' ? '1.35' : '1.2'} fill="none"></path>
                                {/* Lite prediction line */}
                                <path d={predictionsPath} stroke={litePredictionColor} strokeLinecap="round" strokeWidth="1.1" strokeOpacity="0.9" fill="none"></path>
                                {proPredictionsPath && (
                                    <path
                                        d={proPredictionsPath}
                                        stroke={`url(#${proGradientId})`}
                                        strokeLinecap="round"
                                        strokeWidth="1.4"
                                        strokeOpacity="0.74"
                                        fill="none"
                                    ></path>
                                )}
                                {renderPredictionDeltaConnectorLines(predictions, 'lite-delta', 0)}
                                {renderPredictionDeltaConnectorLines(proPredictions, 'pro-delta', 1)}
                                {renderTradeMarkerAnchors()}
                            </>
                        )}
                        {isChangeMode && (
                            <>
                                {actualChangePath && (
                                    <path d={`${actualChangePath} V${chartHeight} H0 Z`} fill={`url(#${actualChangeGradientId})`} stroke="none"></path>
                                )}
                                {actualChangePath && (
                                    <path
                                        d={actualChangePath}
                                        stroke={actualStroke}
                                        strokeLinecap="round"
                                        strokeWidth="1.25"
                                        strokeOpacity="0.94"
                                        fill="none"
                                    ></path>
                                )}
                                {predictedChangePath && (
	                                    <path
	                                        d={predictedChangePath}
	                                        stroke={litePredictionColor}
	                                        strokeLinecap="round"
	                                        strokeWidth="1.25"
                                        strokeOpacity="0.9"
                                        fill="none"
                                    ></path>
                                )}
                                {proPredictedChangePath && (
                                    <path
                                        d={proPredictedChangePath}
                                        stroke={`url(#${proGradientId})`}
                                        strokeLinecap="round"
                                        strokeWidth="1.15"
                                        strokeOpacity="0.86"
                                        fill="none"
                                    ></path>
                                )}
                            </>
                        )}
                        {isChangeMode && !hasChangeSeries && (
                            <text
                                x={width / 2}
                                y={chartHeight / 2}
                                fill="#94A3B8"
                                fontSize={emptyLabelFontSize}
                                fontWeight="600"
                                textAnchor="middle"
                            >
                                {language === 'zh' ? '暂无涨跌幅数据' : 'No change data'}
                            </text>
                        )}

                        {/* Hover Line and Dots */}
                        {hoverIndex !== null && (
                            <>
                                <line 
                                    x1={hoverIndex * stepX} 
                                    y1={0} 
                                    x2={hoverIndex * stepX} 
                                    y2={chartHeight} 
                                    stroke="#ffffff" 
                                    strokeOpacity="0.2" 
                                    strokeWidth="1" 
                                    strokeDasharray="2 2"
	                                />
	                                {!isChangeMode && actuals[hoverIndex] !== undefined && actuals[hoverIndex] !== null && actuals[hoverIndex] !== 0 && (
	                                    <circle cx={hoverIndex * stepX} cy={getPriceY(actuals[hoverIndex])} r="3" fill={actualStroke} stroke="#1f2937" strokeWidth="1" />
	                                )}
	                                {!isChangeMode && predictions[hoverIndex] !== undefined && predictions[hoverIndex] !== null && predictions[hoverIndex] !== 0 && (
	                                    <circle cx={hoverIndex * stepX} cy={getPriceY(predictions[hoverIndex])} r="3" fill={litePredictionColor} fillOpacity="0.94" stroke="#1f2937" strokeWidth="1" />
	                                )}
	                                {!isChangeMode && proPredictions[hoverIndex] !== undefined && proPredictions[hoverIndex] !== null && proPredictions[hoverIndex] !== 0 && (
	                                    <circle
	                                        cx={hoverIndex * stepX}
	                                        cy={getPriceY(proPredictions[hoverIndex])}
	                                        r="3.2"
	                                        fill={proAccentColor}
	                                        fillOpacity="0.92"
	                                        stroke="#1f2937"
	                                        strokeWidth="1"
	                                    />
	                                )}
	                                {isChangeMode && isFinitePoint(actualChangePercents[hoverIndex]) && (
	                                    <circle cx={hoverIndex * stepX} cy={getChangeY(actualChangePercents[hoverIndex])} r="2.4" fill={actualStroke} stroke="#1f2937" strokeWidth="1" />
	                                )}
	                                {isChangeMode && isFinitePoint(predictedChangePercents[hoverIndex]) && (
	                                    <circle cx={hoverIndex * stepX} cy={getChangeY(predictedChangePercents[hoverIndex])} r="2.4" fill={litePredictionColor} fillOpacity="0.94" stroke="#1f2937" strokeWidth="1" />
	                                )}
	                                {isChangeMode && isFinitePoint(proPredictedChangePercents[hoverIndex]) && (
	                                    <circle cx={hoverIndex * stepX} cy={getChangeY(proPredictedChangePercents[hoverIndex])} r="2.4" fill={proAccentColor} fillOpacity="0.86" stroke="#1f2937" strokeWidth="1" />
	                                )}
	                            </>
	                        )}

                        <defs>
	                            <linearGradient gradientUnits="userSpaceOnUse" id={predictionFillGradientId} x1="0" y1="0" x2="0" y2={chartHeight}>
	                                <stop stopColor={predictionFillIsPro ? '#F59E0B' : litePredictionColor} stopOpacity={predictionFillIsPro ? '0.34' : '0.22'}></stop>
	                                <stop offset="1" stopColor={predictionFillIsPro ? '#F59E0B' : litePredictionColor} stopOpacity="0"></stop>
	                            </linearGradient>
	                            <linearGradient gradientUnits="userSpaceOnUse" id={actualChangeGradientId} x1="0" y1="0" x2="0" y2={chartHeight}>
	                                <stop stopColor="#E5E7EB" stopOpacity="0.34"></stop>
	                                <stop offset="1" stopColor="#E5E7EB" stopOpacity="0"></stop>
	                            </linearGradient>
	                            <linearGradient
                                gradientUnits="userSpaceOnUse"
                                id={proGradientId}
                                x1="0"
                                y1="0"
                                x2={Math.max(width / 6, 1)}
                                y2="0"
                                spreadMethod="repeat"
                            >
                                <stop offset="0%" stopColor="#FFF1B8" stopOpacity="0.95"></stop>
                                <stop offset="50%" stopColor="#F59E0B" stopOpacity="0.9"></stop>
                                <stop offset="100%" stopColor="#FFF1B8" stopOpacity="0.95"></stop>
                            </linearGradient>
                        </defs>
                    </svg>

                    {renderTradeMarkerLabels()}

                    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-[1]">
                        {dateLabels.map(item => (
                            <div
                                key={item.key}
                                className={`absolute bottom-0 whitespace-nowrap font-medium leading-none text-[#9CA3AF] ${item.translateXClass}`}
                                style={{
                                    left: `${item.leftPercent}%`,
                                    fontSize: `${axisLabelFontSize}px`,
                                }}
                            >
                                {isMobileViewport ? (
                                    <div className="flex flex-col items-center" style={{ gap: `${Math.max(1, axisSecondLineOffset - axisLabelFontSize)}px` }}>
                                        <span>{item.label.top}</span>
                                        {item.label.bottom ? <span>{item.label.bottom}</span> : null}
                                    </div>
                                ) : (
                                    item.text
                                )}
                            </div>
                        ))}
                    </div>

                    {!isChangeMode && (
                        <>
                            {renderPredictionDeltaLabels(predictions, 'lite-delta', 0)}
                            {renderPredictionDeltaLabels(proPredictions, 'pro-delta', 1)}
                        </>
                    )}

                    {/* Tooltip */}
                    {hoverIndex !== null && (
	                        <div
	                            ref={tooltipRef}
	                            className="pointer-events-none absolute top-3 z-10 min-w-[132px] rounded border border-white/10 bg-gray-900/90 p-2 shadow-xl backdrop-blur-sm"
                            style={{
                                left: `${tooltipLeft}px`,
                                fontSize: `${tooltipFontSize}px`,
                            }}
                        >
                            <div className="text-white/60 mb-1" style={{ fontSize: `${tooltipMetaFontSize}px` }}>{dates[hoverIndex]}</div>
                            {!isChangeMode && actuals[hoverIndex] !== undefined && actuals[hoverIndex] !== null && actuals[hoverIndex] !== 0 && (
                                <div className="flex items-center gap-2">
                                    <div className="w-2 h-2 rounded-full" style={actualDotStyle}></div>
                                    <span className="text-white">{actualLabel}: {actuals[hoverIndex].toFixed(2)}</span>
                                </div>
                            )}
	                            {!isChangeMode && predictions[hoverIndex] !== undefined && predictions[hoverIndex] !== null && predictions[hoverIndex] !== 0 && (
	                                <div className="flex items-center gap-2">
	                                    <div className="w-2 h-2 rounded-full" style={{ backgroundColor: litePredictionColor }}></div>
	                                    <span className="font-medium text-slate-50">
                                        {predLabel}: {predictions[hoverIndex].toFixed(2)}
                                    </span>
                                </div>
                            )}
                            {!isChangeMode && proPredictions[hoverIndex] !== undefined && proPredictions[hoverIndex] !== null && proPredictions[hoverIndex] !== 0 && (
                                <div className="flex items-center gap-2">
                                    <div
                                        className="w-2 h-2 rounded-full"
                                        style={{ background: 'linear-gradient(135deg, #FFF1B8 0%, #F59E0B 50%, #FFF1B8 100%)' }}
	                                    ></div>
	                                    <span className="font-medium" style={{ color: proAccentColor }}>
	                                        {proLabel}: {proPredictions[hoverIndex].toFixed(2)}
	                                    </span>
	                                </div>
	                            )}
	                            {isChangeMode && isFinitePoint(actualChangePercents[hoverIndex]) && (
	                                <div className="flex items-center gap-2">
	                                    <div className="h-0.5 w-3 rounded-full" style={{ backgroundColor: actualStroke }}></div>
	                                    <span className="font-medium" style={{ color: actualStroke }}>{actualChangeLabel}: {actualChangePercents[hoverIndex].toFixed(2)}%</span>
	                                </div>
	                            )}
	                            {isChangeMode && isFinitePoint(predictedChangePercents[hoverIndex]) && (
	                                <div className="flex items-center gap-2">
	                                    <div className="h-0.5 w-3 rounded-full" style={{ backgroundColor: litePredictionColor }}></div>
	                                    <span className="font-medium text-slate-50">{predChangeLabel}: {predictedChangePercents[hoverIndex].toFixed(2)}%</span>
	                                </div>
	                            )}
	                            {isChangeMode && isFinitePoint(proPredictedChangePercents[hoverIndex]) && (
	                                <div className="flex items-center gap-2">
	                                    <div
	                                        className="h-0.5 w-3 rounded-full"
	                                        style={{ background: 'linear-gradient(90deg, #FFF1B8 0%, #F59E0B 50%, #FFF1B8 100%)' }}
	                                    ></div>
	                                    <span className="font-medium" style={{ color: proAccentColor }}>{proLabel} {predChangeLabel}: {proPredictedChangePercents[hoverIndex].toFixed(2)}%</span>
	                                </div>
	                            )}
	                        </div>
                    )}

                    {renderCornerPriceLabel(startPrice, 'left')}
                    {renderCornerPriceLabel(currentPrice, 'right')}
                </div>
            );
        }
    }

    // Static paths for visual representation (Fallback)
    const paths = {
      positive1: "M0 109C18.15 109 18.15 21 36.3 21C54.46 21 54.46 41 72.6 41C90.77 41 90.77 93 108.9 93C127.07 93 127.07 33 145.2 33C163.38 33 163.38 101 181.5 101C199.69 101 199.69 61 217.8 61C236 61 236 45 254.1 45C272.3 45 272.3 121 290.4 121C308.6 121 308.6 149 326.7 149C344.9 149 344.9 1 363 1C381.2 1 381.2 81 399.3 81C417.5 81 417.5 129 435.6 129C453.8 129 453.8 25 472 25",
      positive2: "M0 129C18.15 129 18.15 1 36.3 1C54.46 1 54.46 149 72.6 149C90.77 149 90.77 61 108.9 61C127.07 61 127.07 101 145.2 101C163.38 101 163.38 21 181.5 21C199.69 21 199.69 93 217.8 93C236 93 236 33 254.1 33C272.3 33 272.3 121 290.4 121C308.6 121 308.6 41 326.7 41C344.9 41 344.9 81 363 81C381.2 81 381.2 25 399.3 25C417.5 25 417.5 109 435.6 109C453.8 109 453.8 45 472 45",
      negative: "M0 45C18.15 45 18.15 121 36.3 121C54.46 121 54.46 101 72.6 101C90.77 101 90.77 61 108.9 61C127.07 61 127.07 93 145.2 93C163.38 93 163.38 33 181.5 33C199.69 33 199.69 61 217.8 61C236 61 236 21 254.1 21C272.3 21 272.3 81 290.4 81C308.6 81 308.6 1 326.7 1C344.9 1 344.9 149 363 149C381.2 149 381.2 109 399.3 109C417.5 109 417.5 41 435.6 41C453.8 41 453.8 129 472 129",
    };
    const pathData = isPositive ? (change > 1 ? paths.positive2 : paths.positive1) : paths.negative;

    return (
        <svg fill="none" height="100%" preserveAspectRatio="none" viewBox="-3 0 478 150" width="100%" xmlns="http://www.w3.org/2000/svg">
            <path d={`${pathData}V149H0Z`} fill={`url(#paint_${isPositive ? 'positive' : 'negative'})`}></path>
            <path d={pathData} stroke={color} strokeLinecap="round" strokeWidth="3"></path>
            <defs>
                <linearGradient gradientUnits="userSpaceOnUse" id={`paint_${isPositive ? 'positive' : 'negative'}`} x1="236" x2="236" y1="1" y2="149">
                    <stop stopColor={color} stopOpacity="0.3"></stop>
                    <stop offset="1" stopColor={color} stopOpacity="0"></stop>
                </linearGradient>
            </defs>
        </svg>
    )
};

export default PredictionChart;
