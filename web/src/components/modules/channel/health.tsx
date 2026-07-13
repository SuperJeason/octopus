'use client';

import { useMemo, useState } from 'react';
import { Activity, ChevronDown, Clock3, LoaderCircle, Play, Search } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';
import { useGroupHealthEnabled } from '@/api/endpoints/setting';
import {
    useChannelHealth,
    useRunChannelHealth,
    type ChannelHealthAttempt,
    type ChannelHealthAttemptStatus,
    type ChannelHealthStatus,
} from '@/api/endpoints/channel-health';

function formatDateTime(value?: string | null) {
    if (!value) return 'Never';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Never';
    return date.toLocaleString();
}

function formatRelativeTime(value: string | null | undefined, locale: string, fallback: string) {
    if (!value) return fallback;
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return fallback;

    const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
    const absSeconds = Math.abs(diffSeconds);
    const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'always' });

    if (absSeconds < 60) return formatter.format(diffSeconds, 'second');
    const diffMinutes = Math.round(diffSeconds / 60);
    if (Math.abs(diffMinutes) < 60) return formatter.format(diffMinutes, 'minute');
    const diffHours = Math.round(diffMinutes / 60);
    if (Math.abs(diffHours) < 24) return formatter.format(diffHours, 'hour');
    const diffDays = Math.round(diffHours / 24);
    return formatter.format(diffDays, 'day');
}

function statusLabel(status?: ChannelHealthStatus | null) {
    return status ?? 'idle';
}

function statusDotTone(status?: ChannelHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'bg-emerald-500';
        case 'partial':
            return 'bg-amber-500';
        case 'running':
            return 'bg-sky-500 animate-pulse';
        case 'failed':
            return 'bg-destructive';
        default:
            return 'bg-muted-foreground/40';
    }
}

function statusTextTone(status?: ChannelHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'text-emerald-600 dark:text-emerald-400';
        case 'partial':
            return 'text-amber-600 dark:text-amber-400';
        case 'running':
            return 'text-sky-600 dark:text-sky-400';
        case 'failed':
            return 'text-destructive';
        default:
            return 'text-muted-foreground';
    }
}

function attemptBadgeTone(status: ChannelHealthAttemptStatus) {
    switch (status) {
        case 'success':
            return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'skipped':
            return 'border-border bg-muted/40 text-muted-foreground';
        case 'failed':
        default:
            return 'border-destructive/20 bg-destructive/10 text-destructive';
    }
}

export function splitChannelModels(...values: Array<string | null | undefined>) {
    const seen = new Set<string>();
    const result: string[] = [];
    for (const value of values) {
        for (const part of (value ?? '').split(',')) {
            const name = part.trim();
            if (!name || seen.has(name)) continue;
            seen.add(name);
            result.push(name);
        }
    }
    return result;
}

function ChannelHealthAttemptDetails({ attempt }: { attempt: ChannelHealthAttempt }) {
    const t = useTranslations('channel.health');
    const hasError = Boolean(attempt.error_message);

    const content = (
        <div className="grid grid-cols-[1rem_minmax(0,1fr)_auto] items-start gap-x-2 text-xs">
            <div className="flex h-5 items-center justify-center text-muted-foreground">
                {hasError ? <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" /> : null}
            </div>
            <div className="min-w-0">
                <div className="truncate font-medium leading-5">
                    {attempt.model_name}
                    {attempt.key_remark ? ` / ${attempt.key_remark}` : ''}
                </div>
                <div className="mt-1 flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap leading-4 text-muted-foreground">
                    <span className="shrink-0">{attempt.http_status ? `HTTP ${attempt.http_status}` : t('noHttpStatus')}</span>
                    <span className="shrink-0">·</span>
                    <span className="shrink-0">{attempt.duration_ms}ms</span>
                </div>
            </div>
            <Badge variant="outline" className={cn('shrink-0 text-[11px]', attemptBadgeTone(attempt.status))}>
                {t(`attemptStatus.${attempt.status}`)}
            </Badge>
        </div>
    );

    if (!hasError) {
        return (
            <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
                <CardContent className="px-3 py-2 text-xs">
                    {content}
                </CardContent>
            </Card>
        );
    }

    return (
        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
            <details className="group">
                <summary className="cursor-pointer list-none px-3 py-2 text-xs [&::-webkit-details-marker]:hidden">
                    {content}
                </summary>
                <div className="mx-3 mb-2 ml-9 max-h-36 overflow-y-auto whitespace-pre-wrap break-all border-t border-border/60 pt-2 text-xs leading-relaxed text-muted-foreground">
                    <div className="mb-1 font-medium text-foreground">{t('errorDetails')}</div>
                    {attempt.error_message}
                </div>
            </details>
        </Card>
    );
}

export function ChannelHealthPanel({
    channelId,
    models = [],
}: {
    channelId?: number;
    models?: string[];
}) {
    const t = useTranslations('channel.health');
    const locale = useLocale();
    const { enabled } = useGroupHealthEnabled();
    const { data: view } = useChannelHealth(channelId ?? null);
    const runChannelHealth = useRunChannelHealth();
    const [detailOpen, setDetailOpen] = useState(false);
    // 内联选择面板：避免嵌在 MorphingDialog 里再开一层 Dialog 被遮挡/点穿
    const [pickerOpen, setPickerOpen] = useState(false);
    const [selected, setSelected] = useState<string[]>([]);
    const [query, setQuery] = useState('');

    // hooks 必须在 early return 之前，保持调用顺序稳定
    const availableModels = useMemo(() => models.filter(Boolean), [models]);
    const filteredModels = useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return availableModels;
        return availableModels.filter((name) => name.toLowerCase().includes(q));
    }, [availableModels, query]);

    if (!enabled || !channelId) return null;

    const latest = view?.latest ?? null;
    const attempts = latest?.attempts ?? [];
    const successCount = latest?.success_count ?? attempts.filter((a) => a.status === 'success').length;
    const totalCount = latest?.model_count || attempts.length || 0;
    const isRunning = latest?.status === 'running';
    const isRunPending = runChannelHealth.isPending
        && runChannelHealth.variables?.channelId === channelId;
    const lastRunRelative = formatRelativeTime(latest?.finished_at ?? latest?.started_at ?? null, locale, t('never'));

    const allFilteredSelected = filteredModels.length > 0
        && filteredModels.every((name) => selected.includes(name));

    const openPicker = () => {
        // 在事件处理里初始化选择状态，避免 effect 内同步 setState
        setSelected(availableModels);
        setQuery('');
        setPickerOpen(true);
    };

    const togglePicker = () => {
        if (pickerOpen) {
            setPickerOpen(false);
            return;
        }
        openPicker();
    };

    const toggleModel = (name: string) => {
        setSelected((prev) => (
            prev.includes(name) ? prev.filter((item) => item !== name) : [...prev, name]
        ));
    };

    const toggleAllFiltered = () => {
        setSelected((prev) => {
            if (allFilteredSelected) {
                const drop = new Set(filteredModels);
                return prev.filter((name) => !drop.has(name));
            }
            const next = new Set(prev);
            for (const name of filteredModels) next.add(name);
            return Array.from(next);
        });
    };

    const handleStartProbe = () => {
        if (!selected.length) return;
        runChannelHealth.mutate(
            { channelId, models: selected },
            {
                onSuccess: () => {
                    setPickerOpen(false);
                },
            },
        );
    };

    return (
        <div className="space-y-2">
            <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
                <Card className="gap-0 rounded-xl border-border/70 bg-background/80 py-0 shadow-none">
                    <CardContent className="flex items-center justify-between gap-2 px-3 py-1.5">
                        <DialogTrigger asChild>
                            <button type="button" className="grid min-w-0 flex-1 grid-cols-[auto_auto_minmax(0,1fr)] items-center gap-x-2 gap-y-0.5 text-left">
                                <span className={cn('row-span-2 size-2 rounded-full self-center', statusDotTone(latest?.status))} />
                                <span className="text-sm font-medium leading-5 text-foreground">{t('title')}</span>
                                <span className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
                                    {lastRunRelative}
                                </span>
                                <span className="col-start-2 col-span-2 flex min-w-0 items-center gap-3 text-xs leading-4 text-muted-foreground">
                                    <span className="inline-flex items-center gap-1">
                                        <Activity className="size-3.5" />
                                        {successCount}/{totalCount}
                                    </span>
                                    <span className="inline-flex items-center gap-1">
                                        <Clock3 className="size-3.5" />
                                        {latest?.duration_ms ?? 0}ms
                                    </span>
                                </span>
                            </button>
                        </DialogTrigger>
                        <Button
                            type="button"
                            size="sm"
                            variant={pickerOpen ? 'default' : 'outline'}
                            className="h-7 rounded-lg px-2 text-xs"
                            disabled={isRunPending || isRunning || availableModels.length === 0}
                            onClick={(e) => {
                                e.preventDefault();
                                e.stopPropagation();
                                togglePicker();
                            }}
                        >
                            {isRunning || isRunPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                            {pickerOpen ? t('pickerCollapse') : t('run')}
                        </Button>
                    </CardContent>
                </Card>

                <DialogContent className="z-[100] flex h-[min(85vh,42rem)] flex-col overflow-hidden rounded-3xl sm:max-w-2xl">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <span className={cn('size-2.5 rounded-full', statusDotTone(latest?.status))} />
                            {t('detailTitle')}
                        </DialogTitle>
                        <DialogDescription>
                            {t('lastRun', { time: formatDateTime(latest?.finished_at ?? latest?.started_at ?? null) })}
                            {latest?.message ? ` · ${latest.message}` : ''}
                        </DialogDescription>
                    </DialogHeader>

                    <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
                        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                            <CardContent className="p-3">
                                <div className="text-xs text-muted-foreground">{t('status')}</div>
                                <div className={cn('mt-1 font-medium', statusTextTone(latest?.status))}>{t(`statusValue.${statusLabel(latest?.status)}`)}</div>
                            </CardContent>
                        </Card>
                        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                            <CardContent className="p-3">
                                <div className="text-xs text-muted-foreground">{t('healthy')}</div>
                                <div className="mt-1 font-medium">{successCount}/{totalCount}</div>
                            </CardContent>
                        </Card>
                        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                            <CardContent className="p-3">
                                <div className="text-xs text-muted-foreground">{t('duration')}</div>
                                <div className="mt-1 font-medium">{latest?.duration_ms ?? 0}ms</div>
                            </CardContent>
                        </Card>
                        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                            <CardContent className="p-3">
                                <div className="text-xs text-muted-foreground">{t('attempts')}</div>
                                <div className="mt-1 font-medium">{attempts.length}</div>
                            </CardContent>
                        </Card>
                    </div>

                    <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
                        {attempts.length ? attempts.map((attempt) => (
                            <ChannelHealthAttemptDetails key={attempt.id} attempt={attempt} />
                        )) : (
                            <div className="rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-6 text-center text-xs text-muted-foreground">
                                {t('empty')}
                            </div>
                        )}
                    </div>
                </DialogContent>
            </Dialog>

            {pickerOpen ? (
                <Card className="gap-0 rounded-2xl border-primary/20 bg-primary/5 py-0 shadow-none">
                    <CardContent className="space-y-3 p-3">
                        <div className="flex flex-wrap items-start justify-between gap-2">
                            <div className="min-w-0 space-y-0.5">
                                <div className="text-sm font-medium text-foreground">{t('pickerTitle')}</div>
                                <div className="text-xs text-muted-foreground">
                                    {t('pickerDescription', { count: availableModels.length })}
                                </div>
                            </div>
                            <div className="text-xs text-muted-foreground">
                                {t('pickerSelected', { selected: selected.length, total: availableModels.length })}
                            </div>
                        </div>

                        <div className="relative">
                            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                            <Input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder={t('pickerSearch')}
                                className="rounded-xl bg-background pl-9"
                            />
                        </div>

                        <div className="flex flex-wrap items-center gap-2">
                            <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={toggleAllFiltered}>
                                {allFilteredSelected ? t('pickerClearFiltered') : t('pickerSelectFiltered')}
                            </Button>
                            <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setSelected(availableModels)}>
                                {t('pickerSelectAll')}
                            </Button>
                            <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={() => setSelected([])}>
                                {t('pickerClearAll')}
                            </Button>
                        </div>

                        <div className="max-h-56 space-y-1 overflow-y-auto rounded-2xl border border-border/70 bg-background p-2">
                            {availableModels.length === 0 ? (
                                <div className="px-3 py-8 text-center text-xs text-muted-foreground">{t('pickerEmpty')}</div>
                            ) : filteredModels.length === 0 ? (
                                <div className="px-3 py-8 text-center text-xs text-muted-foreground">{t('pickerNoMatch')}</div>
                            ) : filteredModels.map((name) => {
                                const checked = selected.includes(name);
                                return (
                                    <label
                                        key={name}
                                        className={cn(
                                            'flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2 text-sm transition-colors',
                                            checked ? 'bg-primary/10 text-foreground' : 'hover:bg-muted/50 text-muted-foreground',
                                        )}
                                    >
                                        <input
                                            type="checkbox"
                                            className="size-4 accent-primary"
                                            checked={checked}
                                            onChange={() => toggleModel(name)}
                                        />
                                        <span className="min-w-0 flex-1 break-all font-medium text-foreground">{name}</span>
                                    </label>
                                );
                            })}
                        </div>

                        <div className="flex flex-wrap justify-end gap-2">
                            <Button type="button" variant="outline" className="rounded-xl" onClick={() => setPickerOpen(false)}>
                                {t('pickerCancel')}
                            </Button>
                            <Button
                                type="button"
                                className="rounded-xl"
                                disabled={!selected.length || isRunPending || isRunning}
                                onClick={handleStartProbe}
                            >
                                {isRunPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                                {t('pickerConfirm', { count: selected.length })}
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            ) : null}
        </div>
    );
}
