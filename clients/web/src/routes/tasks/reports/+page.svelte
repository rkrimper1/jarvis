<script lang="ts">
	import { onMount } from 'svelte';
	import {
		tasks,
		type TransitionCount,
		type DailyThroughput,
		type StatusLogEntry,
		type Task,
		type AssigneeVelocityItem,
		type SprintStatusReport,
		type EodReport,
		type ReporterUsageItem,
		type Sprint
	} from '$lib/api/client';

	type ReportTab = 'transitions' | 'throughput' | 'history' | 'velocity' | 'sprint-status' | 'eod' | 'reporter';
	let tab = $state<ReportTab>('transitions');

	// ── Transition report ─────────────────────────────────────────────
	let transitions     = $state<TransitionCount[]>([]);
	let transLoading    = $state(false);
	let transError      = $state('');

	// ── Throughput report ─────────────────────────────────────────────
	let buckets         = $state<DailyThroughput[]>([]);
	let throughLoading  = $state(false);
	let throughError    = $state('');
	let days            = $state(30);

	// ── Task history ──────────────────────────────────────────────────
	let allTasks        = $state<Task[]>([]);
	let selectedTaskId  = $state('');
	let logEntries      = $state<StatusLogEntry[]>([]);
	let histLoading     = $state(false);
	let histError       = $state('');

	// ── Assignee velocity ─────────────────────────────────────────────
	let velocities      = $state<AssigneeVelocityItem[]>([]);
	let velLoading      = $state(false);
	let velError        = $state('');

	// ── Sprint status ─────────────────────────────────────────────────
	let sprintList      = $state<Sprint[]>([]);
	let selectedSprint  = $state('');
	let sprintStatus    = $state<SprintStatusReport | null>(null);
	let ssLoading       = $state(false);
	let ssError         = $state('');

	// ── End of day ────────────────────────────────────────────────────
	let eodReport       = $state<EodReport | null>(null);
	let eodLoading      = $state(false);
	let eodError        = $state('');
	let eodSprint       = $state('');

	// ── Reporter usage ────────────────────────────────────────────────
	let reporterItems   = $state<ReporterUsageItem[]>([]);
	let repLoading      = $state(false);
	let repError        = $state('');

	// Status display helpers
	const STATUS_LABEL: Record<string, string> = {
		unassigned:  'UNASSIGNED',
		assigned:    'ASSIGNED',
		in_progress: 'IN PROGRESS',
		testing:     'TESTING',
		review:      'REVIEW',
		completed:   'COMPLETED',
		'':          '—',
	};
	const STATUS_COLOR: Record<string, string> = {
		unassigned:  '#555',
		assigned:    '#00d4ff',
		in_progress: '#ffaa00',
		testing:     '#ff6600',
		review:      '#9b59b6',
		completed:   '#2ecc71',
		'':          '#555',
	};

	function statusLabel(s: string)  { return STATUS_LABEL[s]  ?? s.toUpperCase(); }
	function statusColor(s: string)  { return STATUS_COLOR[s]  ?? '#888'; }

	// ── Load on mount ─────────────────────────────────────────────────
	onMount(() => {
		loadTransitions();
		tasks.listAllTasks()
			.then(r => {
				allTasks = r.tasks ?? [];
				if (allTasks.length > 0) selectedTaskId = allTasks[0].taskId;
			})
			.catch(() => {});
		tasks.listSprints()
			.then(r => { sprintList = r.sprints ?? []; })
			.catch(() => {});
	});

	function loadTransitions() {
		transLoading = true;
		transError   = '';
		tasks.getTransitionReport()
			.then(r => { transitions = r.transitions ?? []; })
			.catch(e => { transError = e.message; })
			.finally(() => { transLoading = false; });
	}

	function loadThroughput() {
		throughLoading = true;
		throughError   = '';
		tasks.getThroughputReport(days)
			.then(r => { buckets = r.buckets ?? []; })
			.catch(e => { throughError = e.message; })
			.finally(() => { throughLoading = false; });
	}

	function loadHistory() {
		if (!selectedTaskId) return;
		histLoading = true;
		histError   = '';
		logEntries  = [];
		tasks.getTaskStatusLog(selectedTaskId)
			.then(r => { logEntries = r.entries ?? []; })
			.catch(e => { histError = e.message; })
			.finally(() => { histLoading = false; });
	}

	function loadVelocity() {
		velLoading = true;
		velError   = '';
		tasks.getAssigneeVelocityReport()
			.then(r => { velocities = r.velocities ?? []; })
			.catch(e => { velError = e.message; })
			.finally(() => { velLoading = false; });
	}

	function loadSprintStatus() {
		ssLoading   = true;
		ssError     = '';
		sprintStatus = null;
		tasks.getSprintStatusReport(selectedSprint)
			.then(r => { sprintStatus = r; })
			.catch(e => { ssError = e.message; })
			.finally(() => { ssLoading = false; });
	}

	function loadEod() {
		eodLoading = true;
		eodError   = '';
		eodReport  = null;
		tasks.getEndOfDayReport(eodSprint)
			.then(r => { eodReport = r; })
			.catch(e => { eodError = e.message; })
			.finally(() => { eodLoading = false; });
	}

	function loadReporter() {
		repLoading = true;
		repError   = '';
		tasks.getReporterUsageReport()
			.then(r => { reporterItems = r.items ?? []; })
			.catch(e => { repError = e.message; })
			.finally(() => { repLoading = false; });
	}

	// Re-load throughput when days changes
	$effect(() => { days; loadThroughput(); });
	// Load history when task selection changes
	$effect(() => { if (selectedTaskId) loadHistory(); });
	// Load sprint status when selected sprint changes
	$effect(() => { selectedSprint; if (tab === 'sprint-status') loadSprintStatus(); });

	// ── Derived: transition matrix ────────────────────────────────────
	const STATUSES = ['unassigned','assigned','in_progress','testing','review','completed'];

	function cellCount(from: string, to: string): number {
		return transitions.find(t => t.fromStatus === from && t.toStatus === to)?.count ?? 0;
	}

	// ── Derived: throughput bar chart ─────────────────────────────────
	let maxCount = $derived(Math.max(1, ...buckets.map(b => b.count)));

	// ── Tab switch side effects ───────────────────────────────────────
	function switchTab(t: ReportTab) {
		tab = t;
		if (t === 'velocity'      && velocities.length === 0)    loadVelocity();
		if (t === 'sprint-status' && sprintStatus === null)       loadSprintStatus();
		if (t === 'eod'           && eodReport === null)          loadEod();
		if (t === 'reporter'      && reporterItems.length === 0)  loadReporter();
	}
</script>

<div class="reports-page">
	<div class="report-tabs">
		{#each ([
			['transitions','TRANSITIONS'],
			['throughput','THROUGHPUT'],
			['history','TASK HISTORY'],
			['velocity','VELOCITY'],
			['sprint-status','SPRINT STATUS'],
			['eod','END OF DAY'],
			['reporter','REPORTER']
		] as [ReportTab,string][]) as [id, label]}
			<button class="hud-btn report-tab" class:active={tab === id} onclick={() => switchTab(id)}>
				{label}
			</button>
		{/each}
	</div>

	<!-- ── Transition Matrix ─────────────────────────────────────────── -->
	{#if tab === 'transitions'}
		<div class="section-header">
			<div class="hud-label">STATUS TRANSITION MATRIX</div>
			<div class="text-muted" style="font-size:10px">How many times each from→to transition occurred</div>
			<button class="hud-btn" style="font-size:9px;padding:3px 10px" onclick={loadTransitions}>REFRESH</button>
		</div>
		{#if transLoading}
			<div class="scan-bar"></div>
		{:else if transError}
			<div class="report-error">{transError}</div>
		{:else}
			<div class="matrix-wrap">
				<table class="matrix-table">
					<thead>
						<tr>
							<th class="matrix-corner">FROM \ TO</th>
							{#each STATUSES as to}
								<th class="matrix-col-head" style="color:{statusColor(to)}">{statusLabel(to)}</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each STATUSES as from}
							<tr>
								<td class="matrix-row-head" style="color:{statusColor(from)}">{statusLabel(from)}</td>
								{#each STATUSES as to}
									{@const n = cellCount(from, to)}
									<td class="matrix-cell" class:matrix-cell-zero={n === 0}
										style={n > 0 ? `background:${statusColor(to)}18;color:${statusColor(to)}` : ''}>
										{n > 0 ? n : '·'}
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			{#if transitions.length === 0}
				<div class="empty-state">No transitions logged yet — status changes are recorded automatically.</div>
			{/if}
		{/if}

	<!-- ── Throughput ────────────────────────────────────────────────── -->
	{:else if tab === 'throughput'}
		<div class="section-header">
			<div class="hud-label">DAILY THROUGHPUT</div>
			<div class="through-controls">
				{#each [7, 14, 30, 90] as d}
					<button class="hud-btn" class:active={days === d}
						style="font-size:9px;padding:3px 10px"
						onclick={() => days = d}>{d}D</button>
				{/each}
			</div>
		</div>
		{#if throughLoading}
			<div class="scan-bar"></div>
		{:else if throughError}
			<div class="report-error">{throughError}</div>
		{:else if buckets.length === 0}
			<div class="empty-state">No completions in the last {days} days.</div>
		{:else}
			<div class="chart-wrap">
				{#each buckets as b}
					<div class="bar-col">
						<div class="bar-label-top">{b.count}</div>
						<div class="bar-outer">
							<div class="bar-fill" style="height:{Math.round((b.count / maxCount) * 100)}%"></div>
						</div>
						<div class="bar-label-date">{b.date.slice(5)}</div>
						{#if b.storyPoints > 0}
							<div class="bar-label-sp">{b.storyPoints}pt</div>
						{/if}
					</div>
				{/each}
			</div>
			<div class="through-summary">
				<span class="hud-label" style="font-size:10px">TOTAL</span>
				<span class="text-cyan">{buckets.reduce((s,b) => s+b.count, 0)} tasks</span>
				<span class="text-muted">·</span>
				<span class="text-amber">{buckets.reduce((s,b) => s+b.storyPoints, 0)} story points</span>
				<span class="text-muted">in {days} days</span>
			</div>
		{/if}

	<!-- ── Task History ──────────────────────────────────────────────── -->
	{:else if tab === 'history'}
		<div class="section-header">
			<div class="hud-label">TASK STATUS HISTORY</div>
			<select class="hud-input task-select" bind:value={selectedTaskId}>
				{#if allTasks.length === 0}
					<option value="">— no tasks —</option>
				{:else}
					{#each allTasks as t}
						<option value={t.taskId}>#{t.displayId} {t.title}</option>
					{/each}
				{/if}
			</select>
		</div>
		{#if histLoading}
			<div class="scan-bar"></div>
		{:else if histError}
			<div class="report-error">{histError}</div>
		{:else if logEntries.length === 0}
			<div class="empty-state">No status changes recorded for this task yet.</div>
		{:else}
			<div class="timeline">
				{#each logEntries as entry, i}
					<div class="timeline-row">
						<div class="tl-dot" style="background:{statusColor(entry.toStatus)};box-shadow:0 0 6px {statusColor(entry.toStatus)}"></div>
						{#if i < logEntries.length - 1}
							<div class="tl-line"></div>
						{/if}
						<div class="tl-body">
							<div class="tl-transition">
								{#if entry.fromStatus}
									<span class="tl-status" style="color:{statusColor(entry.fromStatus)}">{statusLabel(entry.fromStatus)}</span>
									<span class="tl-arrow">→</span>
								{/if}
								<span class="tl-status" style="color:{statusColor(entry.toStatus)}">{statusLabel(entry.toStatus)}</span>
							</div>
							<div class="tl-meta">
								{#if entry.changedById}
									<span class="tl-user">{entry.changedById}</span>
								{/if}
								<span class="tl-time">{new Date(entry.changedAt).toLocaleString()}</span>
							</div>
						</div>
					</div>
				{/each}
			</div>
		{/if}

	<!-- ── Assignee Velocity ──────────────────────────────────────── -->
	{:else if tab === 'velocity'}
		<div class="section-header">
			<div class="hud-label">ASSIGNEE VELOCITY</div>
			<div class="text-muted" style="font-size:10px">Avg story points completed per sprint, per assignee</div>
			<button class="hud-btn" style="font-size:9px;padding:3px 10px" onclick={loadVelocity}>REFRESH</button>
		</div>
		{#if velLoading}
			<div class="scan-bar"></div>
		{:else if velError}
			<div class="report-error">{velError}</div>
		{:else if velocities.length === 0}
			<div class="empty-state">No velocity data yet — complete tasks in at least one sprint.</div>
		{:else}
			<div class="vel-table-wrap">
				<table class="vel-table">
					<thead>
						<tr>
							<th class="vel-th">ASSIGNEE</th>
							<th class="vel-th vel-num">AVG PTS</th>
							<th class="vel-th vel-num">STD DEV</th>
							<th class="vel-th">SPRINTS</th>
						</tr>
					</thead>
					<tbody>
						{#each velocities as v}
							<tr class="vel-row">
								<td class="vel-td vel-user">{v.userId}</td>
								<td class="vel-td vel-num text-cyan">{v.avgPoints.toFixed(1)}</td>
								<td class="vel-td vel-num text-muted">±{v.stdDev.toFixed(1)}</td>
								<td class="vel-td">
									<div class="vel-sprints">
										{#each v.sprints as s}
											<span class="vel-sprint-chip" title={s.sprintName}>{s.points}pt</span>
										{/each}
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

	<!-- ── Sprint Status ──────────────────────────────────────────── -->
	{:else if tab === 'sprint-status'}
		<div class="section-header">
			<div class="hud-label">SPRINT STATUS</div>
			<select class="hud-input task-select" bind:value={selectedSprint} onchange={loadSprintStatus}>
				<option value="">— active sprint —</option>
				{#each sprintList as sp}
					<option value={sp.sprintId}>{sp.name}</option>
				{/each}
			</select>
			<button class="hud-btn" style="font-size:9px;padding:3px 10px" onclick={loadSprintStatus}>REFRESH</button>
		</div>
		{#if ssLoading}
			<div class="scan-bar"></div>
		{:else if ssError}
			<div class="report-error">{ssError}</div>
		{:else if !sprintStatus}
			<div class="empty-state">Select a sprint or click REFRESH.</div>
		{:else}
			<div class="ss-header">
				<span class="hud-label">{sprintStatus.sprintName}</span>
				<span class="text-muted" style="font-size:10px">{sprintStatus.totalTasks} tasks · {sprintStatus.totalPoints} pts</span>
			</div>
			<div class="ss-buckets">
				{#each sprintStatus.buckets as b}
					{@const pct = sprintStatus.totalTasks > 0 ? Math.round((b.taskCount / sprintStatus.totalTasks) * 100) : 0}
					<div class="ss-bucket">
						<div class="ss-bucket-label" style="color:{statusColor(b.status)}">{statusLabel(b.status)}</div>
						<div class="ss-bar-outer">
							<div class="ss-bar-fill" style="width:{pct}%;background:{statusColor(b.status)}"></div>
						</div>
						<div class="ss-bucket-nums">
							<span class="text-cyan">{b.taskCount}</span>
							<span class="text-muted">tasks</span>
							<span class="text-amber">{b.storyPoints}pt</span>
							<span class="text-muted">({pct}%)</span>
						</div>
					</div>
				{/each}
			</div>
		{/if}

	<!-- ── End of Day ─────────────────────────────────────────────── -->
	{:else if tab === 'eod'}
		<div class="section-header">
			<div class="hud-label">END OF DAY REPORT</div>
			<select class="hud-input task-select" bind:value={eodSprint} onchange={loadEod}>
				<option value="">— active sprint —</option>
				{#each sprintList as sp}
					<option value={sp.sprintId}>{sp.name}</option>
				{/each}
			</select>
			<button class="hud-btn" style="font-size:9px;padding:3px 10px" onclick={loadEod}>REFRESH</button>
		</div>
		{#if eodLoading}
			<div class="scan-bar"></div>
		{:else if eodError}
			<div class="report-error">{eodError}</div>
		{:else if !eodReport}
			<div class="empty-state">Select a sprint or click REFRESH.</div>
		{:else}
			<div class="eod-grid">
				<div class="eod-stat">
					<div class="eod-stat-val text-cyan">{eodReport.completedToday}</div>
					<div class="eod-stat-label">completed today</div>
				</div>
				<div class="eod-stat">
					<div class="eod-stat-val text-amber">{eodReport.completedPointsToday}</div>
					<div class="eod-stat-label">points today</div>
				</div>
				<div class="eod-stat">
					<div class="eod-stat-val text-cyan">{eodReport.totalCompleted} / {eodReport.totalSprintTasks}</div>
					<div class="eod-stat-label">sprint tasks done</div>
				</div>
				<div class="eod-stat">
					<div class="eod-stat-val" style="color:{eodReport.closeProbability >= 0.7 ? '#2ecc71' : eodReport.closeProbability >= 0.4 ? '#ffaa00' : '#e74c3c'}">
						{Math.round(eodReport.closeProbability * 100)}%
					</div>
					<div class="eod-stat-label">close probability</div>
				</div>
			</div>
			{#if eodReport.userSummaries.length > 0}
				<div class="hud-label" style="margin-top:12px;font-size:9px">USER ACTIVITY TODAY</div>
				<table class="vel-table" style="margin-top:6px">
					<thead>
						<tr>
							<th class="vel-th">USER</th>
							<th class="vel-th vel-num">COMPLETED</th>
							<th class="vel-th vel-num">POINTS</th>
							<th class="vel-th vel-num">STATUS CHANGES</th>
						</tr>
					</thead>
					<tbody>
						{#each eodReport.userSummaries as u}
							<tr class="vel-row">
								<td class="vel-td vel-user">{u.userId}</td>
								<td class="vel-td vel-num text-cyan">{u.completedToday}</td>
								<td class="vel-td vel-num text-amber">{u.pointsToday}</td>
								<td class="vel-td vel-num text-muted">{u.statusChanges}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
			{#if eodReport.statusChangesToday.length > 0}
				<div class="hud-label" style="margin-top:12px;font-size:9px">STATUS CHANGES TODAY</div>
				<div class="timeline" style="margin-top:6px">
					{#each eodReport.statusChangesToday as c, i}
						<div class="timeline-row">
							<div class="tl-dot" style="background:{statusColor(c.toStatus)};box-shadow:0 0 6px {statusColor(c.toStatus)}"></div>
							{#if i < eodReport.statusChangesToday.length - 1}
								<div class="tl-line"></div>
							{/if}
							<div class="tl-body">
								<div class="tl-transition">
									<span class="text-muted" style="font-size:10px">#{c.displayId}</span>
									<span class="tl-status" style="color:var(--hud-muted);font-size:10px">{c.title}</span>
									<span class="tl-arrow">·</span>
									{#if c.fromStatus}
										<span class="tl-status" style="color:{statusColor(c.fromStatus)}">{statusLabel(c.fromStatus)}</span>
										<span class="tl-arrow">→</span>
									{/if}
									<span class="tl-status" style="color:{statusColor(c.toStatus)}">{statusLabel(c.toStatus)}</span>
								</div>
								<div class="tl-meta">
									{#if c.changedById}<span class="tl-user">{c.changedById}</span>{/if}
									<span class="tl-time">{new Date(c.changedAt).toLocaleTimeString()}</span>
								</div>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		{/if}

	<!-- ── Reporter Usage ─────────────────────────────────────────── -->
	{:else if tab === 'reporter'}
		<div class="section-header">
			<div class="hud-label">REPORTER USAGE</div>
			<div class="text-muted" style="font-size:10px">Tasks reported and on-time completion per reporter</div>
			<button class="hud-btn" style="font-size:9px;padding:3px 10px" onclick={loadReporter}>REFRESH</button>
		</div>
		{#if repLoading}
			<div class="scan-bar"></div>
		{:else if repError}
			<div class="report-error">{repError}</div>
		{:else if reporterItems.length === 0}
			<div class="empty-state">No reporter data yet.</div>
		{:else}
			<div class="vel-table-wrap">
				<table class="vel-table">
					<thead>
						<tr>
							<th class="vel-th">REPORTER</th>
							<th class="vel-th vel-num">TOTAL</th>
							<th class="vel-th vel-num">ON TIME</th>
							<th class="vel-th vel-num">LATE</th>
							<th class="vel-th vel-num">ON TIME %</th>
							<th class="vel-th vel-num">PTS DONE</th>
						</tr>
					</thead>
					<tbody>
						{#each reporterItems as r}
							<tr class="vel-row">
								<td class="vel-td vel-user">{r.reporterId}</td>
								<td class="vel-td vel-num text-muted">{r.totalTasks}</td>
								<td class="vel-td vel-num text-cyan">{r.completedOnTime}</td>
								<td class="vel-td vel-num" style="color:#e74c3c">{r.completedLate}</td>
								<td class="vel-td vel-num" style="color:{r.onTimePct >= 80 ? '#2ecc71' : r.onTimePct >= 50 ? '#ffaa00' : '#e74c3c'}">
									{r.onTimePct.toFixed(0)}%
								</td>
								<td class="vel-td vel-num text-amber">{r.totalPointsCompleted}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</div>

<style>
	/* local override — #4a6a8a is too dark against this background */
	.reports-page { display: flex; flex-direction: column; gap: 16px; --hud-muted: #7ab4cc; }

	/* ── Tabs ─────────────────────────────────────────────────────── */
	.report-tabs { display: flex; gap: 4px; flex-wrap: wrap; }
	.report-tab  { font-size: 10px; padding: 4px 14px; }
	.report-tab.active { color: var(--hud-cyan); border-color: var(--hud-cyan); background: #00d4ff0a; }

	/* ── Section header ───────────────────────────────────────────── */
	.section-header { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
	.report-error   { font-family: var(--font-mono); font-size: 11px; color: var(--hud-red); }
	.empty-state    { font-family: var(--font-mono); font-size: 12px; color: var(--hud-muted); padding: 24px 0; text-align: center; letter-spacing: 0.06em; }

	/* ── Transition matrix ────────────────────────────────────────── */
	.matrix-wrap  { overflow-x: auto; }
	.matrix-table { border-collapse: collapse; font-family: var(--font-mono); font-size: 11px; width: 100%; }
	.matrix-corner, .matrix-col-head, .matrix-row-head {
		padding: 6px 8px; font-size: 10px; letter-spacing: 0.08em; text-align: center;
		border-bottom: 1px solid #2a3f55;
	}
	.matrix-corner   { color: var(--hud-muted); text-align: left; min-width: 90px; }
	.matrix-col-head { font-weight: bold; }
	.matrix-row-head { text-align: left; font-weight: bold; border-right: 1px solid #2a3f55; }
	.matrix-cell {
		text-align: center; padding: 5px 8px; border: 1px solid #ffffff10;
		font-size: 12px; font-weight: bold; min-width: 44px;
	}
	.matrix-cell-zero { color: #3d5f72; }

	/* ── Throughput chart ─────────────────────────────────────────── */
	.through-controls { display: flex; gap: 4px; }
	.chart-wrap {
		display: flex; align-items: flex-end; gap: 4px;
		height: 160px; padding: 8px 0; overflow-x: auto;
	}
	.bar-col  { display: flex; flex-direction: column; align-items: center; gap: 2px; min-width: 32px; }
	.bar-label-top  { font-family: var(--font-mono); font-size: 10px; color: var(--hud-cyan); }
	.bar-outer { flex: 1; width: 20px; background: #ffffff14; display: flex; align-items: flex-end; border-radius: 2px; }
	.bar-fill  { width: 100%; background: var(--hud-cyan); border-radius: 2px; min-height: 2px; transition: height 0.3s ease; }
	.bar-label-date { font-family: var(--font-mono); font-size: 9px; color: var(--hud-muted); }
	.bar-label-sp   { font-family: var(--font-mono); font-size: 9px; color: var(--hud-amber); }
	.through-summary { display: flex; gap: 8px; align-items: center; font-family: var(--font-mono); font-size: 12px; }

	/* ── Task history timeline ────────────────────────────────────── */
	.task-select { max-width: 360px; font-size: 11px; }
	.timeline    { display: flex; flex-direction: column; padding: 4px 0; }
	.timeline-row {
		display: flex; align-items: flex-start; gap: 12px;
		position: relative; padding-bottom: 16px;
	}
	.tl-dot  { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; margin-top: 3px; }
	.tl-line {
		position: absolute; left: 4px; top: 13px; bottom: 0;
		width: 2px; background: #2a3f55;
	}
	.tl-body { display: flex; flex-direction: column; gap: 2px; }
	.tl-transition { display: flex; align-items: center; gap: 6px; }
	.tl-status { font-family: var(--font-mono); font-size: 12px; font-weight: bold; letter-spacing: 0.06em; }
	.tl-arrow  { color: var(--hud-muted); font-size: 12px; }
	.tl-meta   { display: flex; gap: 10px; font-family: var(--font-mono); font-size: 11px; color: var(--hud-muted); }
	.tl-user   { color: var(--hud-amber); }

	/* ── Assignee velocity ───────────────────────────────────────── */
	.vel-table-wrap { overflow-x: auto; }
	.vel-table { border-collapse: collapse; font-family: var(--font-mono); font-size: 11px; width: 100%; }
	.vel-th { padding: 6px 10px; font-size: 10px; letter-spacing: 0.08em; color: var(--hud-muted); border-bottom: 1px solid #2a3f55; text-align: left; }
	.vel-th.vel-num { text-align: right; }
	.vel-row:hover { background: #ffffff08; }
	.vel-td { padding: 6px 10px; border-bottom: 1px solid #ffffff10; color: #c8dde8; }
	.vel-td.vel-num { text-align: right; }
	.vel-user { color: var(--hud-amber); }
	.vel-sprints { display: flex; flex-wrap: wrap; gap: 3px; }
	.vel-sprint-chip {
		font-family: var(--font-mono); font-size: 9px; padding: 1px 5px;
		border: 1px solid #2a3f55; border-radius: 2px; color: var(--hud-muted);
	}

	/* ── Sprint status ───────────────────────────────────────────── */
	.ss-header { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
	.ss-buckets { display: flex; flex-direction: column; gap: 10px; }
	.ss-bucket { display: grid; grid-template-columns: 110px 1fr auto; align-items: center; gap: 10px; }
	.ss-bucket-label { font-family: var(--font-mono); font-size: 10px; font-weight: bold; letter-spacing: 0.08em; }
	.ss-bar-outer { height: 10px; background: #ffffff14; border-radius: 2px; overflow: hidden; }
	.ss-bar-fill { height: 100%; border-radius: 2px; transition: width 0.4s ease; min-width: 2px; }
	.ss-bucket-nums { display: flex; gap: 6px; font-family: var(--font-mono); font-size: 10px; white-space: nowrap; color: #c8dde8; }

	/* ── End of day ──────────────────────────────────────────────── */
	.eod-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
	.eod-stat { background: #ffffff0a; border: 1px solid #2a3f55; border-radius: 3px; padding: 12px; text-align: center; }
	.eod-stat-val { font-family: var(--font-mono); font-size: 22px; font-weight: bold; }
	.eod-stat-label { font-family: var(--font-mono); font-size: 10px; color: var(--hud-muted); letter-spacing: 0.08em; margin-top: 4px; }

	/* ── Scan bar ─────────────────────────────────────────────────── */
	.scan-bar {
		height: 2px; background: var(--hud-cyan);
		animation: scan-sweep 1.2s ease-in-out infinite;
		transform-origin: left;
	}
	@keyframes scan-sweep {
		0%   { transform: scaleX(0); opacity: 1; }
		60%  { transform: scaleX(1); opacity: 1; }
		100% { transform: scaleX(1); opacity: 0; }
	}
</style>
