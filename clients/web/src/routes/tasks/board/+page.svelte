<script lang="ts">
	import { tasks, users, type Task, type Sprint, type User, type TaskStatus, type TaskPriority } from '$lib/api/client';
	import { userId } from '$lib/stores/auth';

	const COLUMNS: { status: TaskStatus; label: string }[] = [
		{ status: 'TASK_STATUS_ASSIGNED',    label: 'ASSIGNED' },
		{ status: 'TASK_STATUS_IN_PROGRESS', label: 'IN PROGRESS' },
		{ status: 'TASK_STATUS_COMPLETED',   label: 'COMPLETED' },
	];

	const STACKED_COLUMNS: { status: TaskStatus; label: string }[] = [
		{ status: 'TASK_STATUS_TESTING', label: 'TESTING' },
		{ status: 'TASK_STATUS_REVIEW',  label: 'REVIEW'  },
	];

	let sprints = $state<Sprint[]>([]);
	let selectedSprintId = $state('');
	let taskList = $state<Task[]>([]);
	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');
	let draggingTaskId = $state('');

	function grouped(status: TaskStatus): Task[] {
		return taskList.filter(t => t.status === status);
	}

	function userDisplayName(id: string): string {
		const u = userList.find(u => u.id === id);
		return u ? (u.displayName || u.username) : id;
	}

	function priorityClass(p: TaskPriority): string {
		switch (p) {
			case 'TASK_PRIORITY_CRITICAL': return 'priority-critical';
			case 'TASK_PRIORITY_HIGH':     return 'priority-high';
			case 'TASK_PRIORITY_MEDIUM':   return 'priority-medium';
			case 'TASK_PRIORITY_LOW':      return 'priority-low';
			default:                        return 'priority-medium';
		}
	}

	function priorityLabel(p: TaskPriority): string {
		switch (p) {
			case 'TASK_PRIORITY_CRITICAL': return 'CRIT';
			case 'TASK_PRIORITY_HIGH':     return 'HIGH';
			case 'TASK_PRIORITY_MEDIUM':   return 'MED';
			case 'TASK_PRIORITY_LOW':      return 'LOW';
			default:                        return 'MED';
		}
	}

	function formatDate(ts: string): string {
		if (!ts) return '';
		try {
			return new Date(ts).toLocaleDateString();
		} catch {
			return ts;
		}
	}

	async function loadSprints() {
		try {
			const res = await sprints.length === 0 ? tasks.listSprints() : Promise.resolve({ sprints });
			// always fresh
			const sp = await tasks.listSprints();
			sprints = sp.sprints ?? [];
			const active = sprints.find(s => s.status === 'SPRINT_STATUS_ACTIVE');
			if (active && !selectedSprintId) {
				selectedSprintId = active.sprintId;
			}
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	async function loadSprintTasks() {
		if (!selectedSprintId) { taskList = []; return; }
		loading = true;
		error = '';
		try {
			const res = await tasks.listSprintTasks(selectedSprintId);
			taskList = res.tasks ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const [sp, ul] = await Promise.all([tasks.listSprints(), users.list()]);
			sprints = sp.sprints ?? [];
			userList = ul.users ?? [];
			const active = sprints.find(s => s.status === 'SPRINT_STATUS_ACTIVE');
			if (active && !selectedSprintId) {
				selectedSprintId = active.sprintId;
			}
			if (selectedSprintId) {
				const tRes = await tasks.listSprintTasks(selectedSprintId);
				taskList = tRes.tasks ?? [];
			}
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function onSprintChange() {
		await loadSprintTasks();
	}

	function onDragStart(e: DragEvent, taskId: string) {
		draggingTaskId = taskId;
		e.dataTransfer?.setData('taskId', taskId);
	}

	function onDragOver(e: DragEvent) {
		e.preventDefault();
	}

	async function onDrop(e: DragEvent, targetStatus: TaskStatus) {
		e.preventDefault();
		const taskId = e.dataTransfer?.getData('taskId') || draggingTaskId;
		if (!taskId) return;

		const task = taskList.find(t => t.taskId === taskId);
		if (!task || task.status === targetStatus) { draggingTaskId = ''; return; }

		// Optimistic update
		const prev = task.status;
		taskList = taskList.map(t => t.taskId === taskId ? { ...t, status: targetStatus } : t);

		try {
			await tasks.moveStatus(taskId, targetStatus, { userId: $userId ?? '' });
		} catch (err) {
			// Revert on error
			taskList = taskList.map(t => t.taskId === taskId ? { ...t, status: prev } : t);
			error = err instanceof Error ? err.message : String(err);
			await loadSprintTasks();
		}
		draggingTaskId = '';
	}

	load();
</script>

<div class="board-page">
	<div class="page-header">
		<div>
			<h1 class="page-title font-hud">TASK MANAGEMENT · BOARD</h1>
		</div>
		<div class="header-actions">
			<a href="/tasks/backlog" class="hud-btn">BACKLOG</a>
			<select class="hud-input sprint-select" bind:value={selectedSprintId} onchange={onSprintChange}>
				<option value="">— select sprint —</option>
				{#each sprints as sp}
					<option value={sp.sprintId}>{sp.name} ({sp.status === 'SPRINT_STATUS_ACTIVE' ? 'ACTIVE' : 'CLOSED'})</option>
				{/each}
			</select>
			<button class="hud-btn" onclick={load} disabled={loading}>
				{loading ? 'LOADING...' : 'REFRESH'}
			</button>
		</div>
	</div>

	{#if error}
		<div class="error-banner">{error}</div>
	{/if}

	{#if !selectedSprintId && !loading}
		<div class="empty-msg font-hud">SELECT A SPRINT TO VIEW THE BOARD</div>
	{:else}
		<div class="board-columns">
			<!-- ASSIGNED, IN PROGRESS -->
			{#each COLUMNS.slice(0, 2) as col}
				<div
					class="board-col"
					ondragover={onDragOver}
					ondrop={(e) => onDrop(e, col.status)}
					role="region"
					aria-label={col.label}
				>
					<div class="col-header font-hud">
						<span class="col-label">{col.label}</span>
						<span class="col-count">{grouped(col.status).length}</span>
					</div>
					<div class="col-cards">
						{#if loading && !taskList.length}
							<div class="col-empty">LOADING...</div>
						{:else if !grouped(col.status).length}
							<div class="col-empty">—</div>
						{:else}
							{#each grouped(col.status) as task}
								<div
									class="task-card"
									draggable="true"
									ondragstart={(e) => onDragStart(e, task.taskId)}
									role="listitem"
								>
									<div class="task-top">
										<span class="task-title">{task.title}</span>
										<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
									</div>
									<div class="task-meta">
										<span class="meta-item">◉ {userDisplayName(task.assigneeId)}</span>
										{#if task.storyPoints > 0}
											<span class="meta-item pts">{task.storyPoints}pt</span>
										{/if}
									</div>
								</div>
							{/each}
						{/if}
					</div>
				</div>
			{/each}

			<!-- TESTING + REVIEW stacked in one column -->
			<div class="board-col stacked-col" role="region" aria-label="Testing and Review">
				{#each STACKED_COLUMNS as col}
					<div
						class="stacked-half"
						ondragover={onDragOver}
						ondrop={(e) => onDrop(e, col.status)}
						role="region"
						aria-label={col.label}
					>
						<div class="col-header font-hud">
							<span class="col-label">{col.label}</span>
							<span class="col-count">{grouped(col.status).length}</span>
						</div>
						<div class="col-cards">
							{#if loading && !taskList.length}
								<div class="col-empty">LOADING...</div>
							{:else if !grouped(col.status).length}
								<div class="col-empty">—</div>
							{:else}
								{#each grouped(col.status) as task}
									<div
										class="task-card"
										draggable="true"
										ondragstart={(e) => onDragStart(e, task.taskId)}
										role="listitem"
									>
										<div class="task-top">
											<span class="task-title">{task.title}</span>
											<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
										</div>
										<div class="task-meta">
											<span class="meta-item">◉ {userDisplayName(task.assigneeId)}</span>
											{#if task.storyPoints > 0}
												<span class="meta-item pts">{task.storyPoints}pt</span>
											{/if}
										</div>
									</div>
								{/each}
							{/if}
						</div>
					</div>
				{/each}
			</div>

			<!-- COMPLETED -->
			{#each COLUMNS.slice(2) as col}
				<div
					class="board-col"
					ondragover={onDragOver}
					ondrop={(e) => onDrop(e, col.status)}
					role="region"
					aria-label={col.label}
				>
					<div class="col-header font-hud">
						<span class="col-label">{col.label}</span>
						<span class="col-count">{grouped(col.status).length}</span>
					</div>
					<div class="col-cards">
						{#if loading && !taskList.length}
							<div class="col-empty">LOADING...</div>
						{:else if !grouped(col.status).length}
							<div class="col-empty">—</div>
						{:else}
							{#each grouped(col.status) as task}
								<div
									class="task-card"
									draggable="true"
									ondragstart={(e) => onDragStart(e, task.taskId)}
									role="listitem"
								>
									<div class="task-top">
										<span class="task-title">{task.title}</span>
										<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
									</div>
									<div class="task-meta">
										<span class="meta-item">◉ {userDisplayName(task.assigneeId)}</span>
										{#if task.storyPoints > 0}
											<span class="meta-item pts">{task.storyPoints}pt</span>
										{/if}
									</div>
									{#if task.completedById}
										<div class="completed-info">
											<span class="meta-item">✓ {userDisplayName(task.completedById)}</span>
											{#if task.completedAt}
												<span class="meta-item">{formatDate(task.completedAt)}</span>
											{/if}
										</div>
									{/if}
								</div>
							{/each}
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.board-page { width: 100%; height: 100%; display: flex; flex-direction: column; }
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 20px; gap: 16px; flex-shrink: 0; }
	.page-title { font-size: 20px; font-weight: 900; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0; }
	.header-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
	.sprint-select { font-family: var(--font-hud, monospace); font-size: 10px; letter-spacing: 0.1em; padding: 6px 10px; min-width: 200px; }
	.error-banner { background: #1a0505; border: 1px solid #ef444466; color: #ef4444; padding: 10px 14px; font-size: 12px; margin-bottom: 16px; flex-shrink: 0; }
	.empty-msg { font-size: 12px; letter-spacing: 0.15em; color: var(--hud-muted); padding: 40px 0; text-align: center; }

	.board-columns {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 12px;
		flex: 1;
		min-height: 0;
		overflow-x: auto;
	}

	.board-col {
		background: #080c14;
		border: 1px solid var(--hud-dim);
		display: flex;
		flex-direction: column;
		min-height: 400px;
		clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.board-col:hover { border-color: #00d4ff22; }

	.stacked-col { min-height: unset; background: transparent; border: none; clip-path: none; gap: 12px; }
	.stacked-col:hover { border-color: transparent; }
	.stacked-half {
		background: #080c14;
		border: 1px solid var(--hud-dim);
		display: flex;
		flex-direction: column;
		flex: 1;
		clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.stacked-half:hover { border-color: #00d4ff22; }

	.col-header { display: flex; align-items: center; justify-content: space-between; padding: 10px 12px 8px; border-bottom: 1px solid var(--hud-dim); flex-shrink: 0; }
	.col-label { font-size: 10px; letter-spacing: 0.2em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); }
	.col-count { font-size: 10px; color: var(--hud-muted); background: #1a2233; padding: 1px 6px; border: 1px solid var(--hud-dim); }

	.col-cards { flex: 1; padding: 10px 8px; display: flex; flex-direction: column; gap: 8px; overflow-y: auto; }
	.col-empty { font-size: 10px; letter-spacing: 0.1em; color: var(--hud-dim); text-align: center; padding: 20px 0; }

	.task-card {
		background: #0d1117;
		border: 1px solid var(--hud-dim);
		padding: 10px 12px;
		cursor: grab;
		transition: border-color 0.15s;
		clip-path: polygon(4px 0%, 100% 0%, calc(100% - 4px) 100%, 0% 100%);
	}
	.task-card:hover { border-color: var(--hud-cyan); }
	.task-card:active { cursor: grabbing; }

	.task-top { display: flex; align-items: flex-start; justify-content: space-between; gap: 6px; margin-bottom: 6px; }
	.task-title { font-size: 11px; color: var(--hud-text); flex: 1; min-width: 0; line-height: 1.3; word-break: break-word; }
	.priority-badge { font-size: 8px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 5px; border: 1px solid; flex-shrink: 0; }
	.priority-critical { color: #ef4444; border-color: #ef444466; }
	.priority-high { color: #f97316; border-color: #f9731666; }
	.priority-medium { color: var(--hud-cyan); border-color: #00d4ff44; }
	.priority-low { color: #6b7280; border-color: #37415166; }

	.task-meta { display: flex; gap: 8px; flex-wrap: wrap; }
	.completed-info { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 4px; border-top: 1px solid var(--hud-dim); padding-top: 4px; }
	.meta-item { font-size: 9px; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.pts { color: #a78bfa; }
</style>
