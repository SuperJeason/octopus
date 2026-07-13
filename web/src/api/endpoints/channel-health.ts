import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import { useGroupHealthEnabled } from './setting';

export type ChannelHealthStatus = 'running' | 'success' | 'partial' | 'failed';
export type ChannelHealthAttemptStatus = 'success' | 'failed' | 'skipped';

export interface ChannelHealthAttempt {
    id: number;
    snapshot_id: number;
    channel_id: number;
    channel_name: string;
    channel_key_id: number;
    key_remark: string;
    model_name: string;
    status: ChannelHealthAttemptStatus;
    http_status: number;
    duration_ms: number;
    error_message: string;
}

export interface ChannelHealthSnapshot {
    id: number;
    channel_id: number;
    channel_name: string;
    status: ChannelHealthStatus;
    started_at: string;
    finished_at?: string | null;
    duration_ms: number;
    message: string;
    model_count: number;
    success_count: number;
    attempts: ChannelHealthAttempt[];
}

export interface ChannelHealthView {
    channel_id: number;
    channel_name: string;
    latest?: ChannelHealthSnapshot | null;
}

export type RunChannelHealthAccepted = {
    channel_id?: number;
    models?: string[];
};

export type RunChannelHealthRequest = {
    channelId: number;
    models?: string[];
};

function normalizeAttempt(attempt: Partial<ChannelHealthAttempt>): ChannelHealthAttempt {
    return {
        id: typeof attempt.id === 'number' ? attempt.id : 0,
        snapshot_id: typeof attempt.snapshot_id === 'number' ? attempt.snapshot_id : 0,
        channel_id: typeof attempt.channel_id === 'number' ? attempt.channel_id : 0,
        channel_name: attempt.channel_name ?? '',
        channel_key_id: typeof attempt.channel_key_id === 'number' ? attempt.channel_key_id : 0,
        key_remark: attempt.key_remark ?? '',
        model_name: attempt.model_name ?? '',
        status: attempt.status === 'success' || attempt.status === 'failed' || attempt.status === 'skipped'
            ? attempt.status
            : 'failed',
        http_status: typeof attempt.http_status === 'number' ? attempt.http_status : 0,
        duration_ms: typeof attempt.duration_ms === 'number' ? attempt.duration_ms : 0,
        error_message: attempt.error_message ?? '',
    };
}

function normalizeSnapshot(snapshot: Partial<ChannelHealthSnapshot> | null | undefined): ChannelHealthSnapshot | null {
    if (!snapshot) return null;
    return {
        id: typeof snapshot.id === 'number' ? snapshot.id : 0,
        channel_id: typeof snapshot.channel_id === 'number' ? snapshot.channel_id : 0,
        channel_name: snapshot.channel_name ?? '',
        status: snapshot.status === 'running' || snapshot.status === 'success' || snapshot.status === 'partial' || snapshot.status === 'failed'
            ? snapshot.status
            : 'failed',
        started_at: snapshot.started_at ?? '',
        finished_at: snapshot.finished_at ?? null,
        duration_ms: typeof snapshot.duration_ms === 'number' ? snapshot.duration_ms : 0,
        message: snapshot.message ?? '',
        model_count: typeof snapshot.model_count === 'number' ? snapshot.model_count : 0,
        success_count: typeof snapshot.success_count === 'number' ? snapshot.success_count : 0,
        attempts: (snapshot.attempts ?? []).map(normalizeAttempt),
    };
}

function normalizeView(view: Partial<ChannelHealthView>): ChannelHealthView {
    return {
        channel_id: typeof view.channel_id === 'number' ? view.channel_id : 0,
        channel_name: view.channel_name ?? '',
        latest: normalizeSnapshot(view.latest),
    };
}

function invalidateChannelHealth(queryClient: ReturnType<typeof useQueryClient>, channelId?: number) {
    queryClient.invalidateQueries({ queryKey: ['channel-health'] });
    if (channelId != null) {
        queryClient.invalidateQueries({ queryKey: ['channel-health', 'detail', channelId] });
    }
}

export function useChannelHealth(channelId: number | null) {
    const { enabled } = useGroupHealthEnabled();
    return useQuery({
        queryKey: ['channel-health', 'detail', channelId],
        queryFn: async () => apiClient.get<ChannelHealthView>(`/api/v1/channel/health/${channelId}`),
        select: normalizeView,
        enabled: enabled && channelId != null && channelId > 0,
        refetchInterval: (query) => {
            const data = query.state.data as ChannelHealthView | undefined;
            return data?.latest?.status === 'running' ? 3000 : 30000;
        },
    });
}

export function useRunChannelHealth() {
    const queryClient = useQueryClient();
    const { enabled } = useGroupHealthEnabled();
    return useMutation({
        mutationFn: async ({ channelId, models }: RunChannelHealthRequest) => {
            if (!enabled) throw new Error('Group health checks are disabled');
            return apiClient.post<RunChannelHealthAccepted>(
                `/api/v1/channel/health/${channelId}/run`,
                models?.length ? { models } : {},
            );
        },
        onSuccess: (_data, variables) => invalidateChannelHealth(queryClient, variables.channelId),
        onError: (error) => logger.error('channel health run failed:', error),
    });
}
