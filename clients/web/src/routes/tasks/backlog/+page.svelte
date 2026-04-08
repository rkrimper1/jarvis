<script lang="ts">
	import { tasks, users, type Task, type Sprint, type User, type TaskPriority } from '$lib/api/client';
	import { userId, canManageSprints } from '$lib/stores/auth';

	let backlog = $state<Task[]>([]);
	let sprints = $state<Sprint[]>([]);
	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');

	// Create task form
	let showCreateTask = $state(false);
	let newTitle = $state('');
	let newDescription = $state('');
	let newAssigneeId = $state('');
	let newPriority = $state<TaskPriority>('TASK_PRIORITY_MEDIUM');
	let newStoryPoints = $state(0);
	let newDueDate = $state('');
	let newSprintId = $state('');
	let createTaskError = $state('');
	let creatingTask = $state(false);

	// Edit task form
	let editingTask = $state<Task | null>(null);
	let editTitle = $state('');
	let editDescription = $state('');
	let editAssigneeId = $state('');
	let editPriority = $state<TaskPriority>('TASK_PRIORITY_MEDIUM');
	let editStoryPoints = $state(0);
	let editDueDate = $state('');
	let editSprintId = $state('');
	let editTaskError = $state('');
	let savingTask = $state(false);

	// Create/edit sprint form
	let showSprintForm = $state(false);
	let editingSprint = $state<Sprint | null>(null);
	let sprintName = $state('');
	let sprintGoal = $state('');
	let sprintStartDate = $state('');
	let sprintEndDate = $state('');
	let sprintFormError = $state('');
	let savingSprint = $state(false);

	// Drag state
	let draggingTaskId = $state('');

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

	function sprintTaskCount(sprintId: string): number {
		// We don't load sprint tasks on this page; show 0 as placeholder
		return 0;
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const [bl, sp, ul] = await Promise.all([
				tasks.listBacklog(),
				tasks.listSprints(),
				users.list()
			]);
			backlog = bl.tasks ?? [];
			sprints = sp.sprints ?? [];
			userList = ul.users ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
		}
	}

	async function submitCreateTask() {
		if (!newTitle.trim()) { createTaskError = 'Title is required.'; return; }
		if (!newAssigneeId) { createTaskError = 'Assignee is required.'; return; }
		createTaskError = '';
		creatingTask = true;
		try {
			await tasks.createTask({
				title: newTitle.trim(),
				description: newDescription.trim(),
				assigneeId: newAssigneeId,
				reporterId: $userId ?? '',
				priority: newPriority,
				storyPoints: newStoryPoints,
				dueDate: newDueDate,
				sprintId: newSprintId || undefined
			});
			newTitle = ''; newDescription = ''; newAssigneeId = ''; newPriority = 'TASK_PRIORITY_MEDIUM';
			newStoryPoints = 0; newDueDate = ''; newSprintId = '';
			showCreateTask = false;
			await load();
		} catch (e) {
			createTaskError = e instanceof Error ? e.message : String(e);
		} finally {
			creatingTask = false;
		}
	}

	function openEditTask(task: Task) {
		editingTask = task;
		editTitle = task.title;
		editDescription = task.description ?? '';
		editAssigneeId = task.assigneeId;
		editPriority = task.priority;
		editStoryPoints = task.storyPoints ?? 0;
		editDueDate = task.dueDate ?? '';
		editSprintId = task.sprintId ?? '';
		editTaskError = '';
	}

	async function submitEditTask() {
		if (!editTitle.trim()) { editTaskError = 'Title is required.'; return; }
		if (!editAssigneeId) { editTaskError = 'Assignee is required.'; return; }
		if (!editingTask) return;
		editTaskError = '';
		savingTask = true;
		try {
			await tasks.updateTask(editingTask.taskId, {
				title: editTitle.trim(),
				description: editDescription.trim(),
				assigneeId: editAssigneeId,
				priority: editPriority,
				storyPoints: editStoryPoints,
				dueDate: editDueDate
			});
			if (editSprintId && editSprintId !== editingTask.sprintId) {
				await tasks.assignToSprint(editingTask.taskId, editSprintId);
			}
			editingTask = null;
			await load();
		} catch (e) {
			editTaskError = e instanceof Error ? e.message : String(e);
		} finally {
			savingTask = false;
		}
	}

	function openSprintCreate() {
		editingSprint = null;
		sprintName = ''; sprintGoal = ''; sprintStartDate = ''; sprintEndDate = '';
		sprintFormError = '';
		showSprintForm = true;
	}

	function openSprintEdit(sp: Sprint) {
		editingSprint = sp;
		sprintName = sp.name; sprintGoal = sp.goal;
		sprintStartDate = sp.startDate; sprintEndDate = sp.endDate;
		sprintFormError = '';
		showSprintForm = true;
	}

	async function submitSprintForm() {
		if (!sprintName.trim()) { sprintFormError = 'Name is required.'; return; }
		sprintFormError = '';
		savingSprint = true;
		try {
			if (editingSprint) {
				await tasks.updateSprint(editingSprint.sprintId, {
					name: sprintName.trim(), goal: sprintGoal.trim(),
					startDate: sprintStartDate, endDate: sprintEndDate
				});
			} else {
				await tasks.createSprint({
					name: sprintName.trim(), goal: sprintGoal.trim(),
					startDate: sprintStartDate, endDate: sprintEndDate
				});
			}
			showSprintForm = false;
			await load();
		} catch (e) {
			sprintFormError = e instanceof Error ? e.message : String(e);
		} finally {
			savingSprint = false;
		}
	}

	async function deleteSprint(sp: Sprint) {
		if (!confirm(`Delete sprint "${sp.name}"?`)) return;
		try {
			await tasks.deleteSprint(sp.sprintId);
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	async function closeSprint(sp: Sprint) {
		if (!confirm(`Close sprint "${sp.name}"? This cannot be undone.`)) return;
		try {
			await tasks.closeSprint(sp.sprintId);
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	function onDragStart(e: DragEvent, taskId: string) {
		draggingTaskId = taskId;
		e.dataTransfer?.setData('taskId', taskId);
	}

	function onDragOver(e: DragEvent) {
		e.preventDefault();
	}

	async function onDropSprint(e: DragEvent, sprintId: string) {
		e.preventDefault();
		const taskId = e.dataTransfer?.getData('taskId') || draggingTaskId;
		if (!taskId) return;
		try {
			await tasks.assignToSprint(taskId, sprintId);
			await load();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
		draggingTaskId = '';
	}

	load();
</script>

<div class="backlog-page">
	<div class="page-header">
		<div>
			<h1 class="page-title font-hud">TASK MANAGEMENT · BACKLOG</h1>
			<p class="page-sub">Drag tasks onto a sprint to assign them</p>
		</div>
		<div class="header-actions">
			<a href="/tasks/board" class="hud-btn">BOARD VIEW</a>
			<button class="hud-btn" onclick={load} disabled={loading}>
				{loading ? 'LOADING...' : 'REFRESH'}
			</button>
		</div>
	</div>

	{#if error}
		<div class="error-banner">{error}</div>
	{/if}

	<div class="two-col">
		<!-- Left: Backlog -->
		<section class="backlog-col">
			<div class="section-header">
				<h2 class="section-title font-hud">BACKLOG <span class="count">({backlog.length})</span></h2>
				<button class="hud-btn small" onclick={() => showCreateTask = !showCreateTask}>
					{showCreateTask ? 'CANCEL' : '+ CREATE TASK'}
				</button>
			</div>

			{#if showCreateTask}
				<div class="create-form">
					<div class="form-title font-hud">NEW TASK</div>
					<div class="field">
						<label class="field-label" for="ct-title">TITLE *</label>
						<input id="ct-title" class="hud-input" bind:value={newTitle} placeholder="Task title..." />
					</div>
					<div class="field">
						<label class="field-label" for="ct-desc">DESCRIPTION</label>
						<textarea id="ct-desc" class="hud-input" bind:value={newDescription} rows="2" placeholder="Details..."></textarea>
					</div>
					<div class="form-row">
						<div class="field">
							<label class="field-label" for="ct-assignee">ASSIGNEE *</label>
							<select id="ct-assignee" class="hud-input" bind:value={newAssigneeId}>
								<option value="">— select —</option>
								{#each userList as u}
									<option value={u.id}>{u.displayName || u.username}</option>
								{/each}
							</select>
						</div>
						<div class="field">
							<label class="field-label" for="ct-priority">PRIORITY</label>
							<select id="ct-priority" class="hud-input" bind:value={newPriority}>
								<option value="TASK_PRIORITY_CRITICAL">CRITICAL</option>
								<option value="TASK_PRIORITY_HIGH">HIGH</option>
								<option value="TASK_PRIORITY_MEDIUM">MEDIUM</option>
								<option value="TASK_PRIORITY_LOW">LOW</option>
							</select>
						</div>
					</div>
					<div class="form-row">
						<div class="field">
							<label class="field-label" for="ct-pts">STORY POINTS</label>
							<input id="ct-pts" class="hud-input" type="number" min="0" bind:value={newStoryPoints} />
						</div>
						<div class="field">
							<label class="field-label" for="ct-due">DUE DATE</label>
							<input id="ct-due" class="hud-input" type="date" bind:value={newDueDate} />
						</div>
					</div>
					<div class="field">
						<label class="field-label" for="ct-sprint">SPRINT (OPTIONAL)</label>
						<select id="ct-sprint" class="hud-input" bind:value={newSprintId}>
							<option value="">— backlog (no sprint) —</option>
							{#each sprints.filter(s => s.status === 'SPRINT_STATUS_ACTIVE') as sp}
								<option value={sp.sprintId}>{sp.name}</option>
							{/each}
						</select>
					</div>
					{#if createTaskError}
						<div class="form-error">{createTaskError}</div>
					{/if}
					<button class="hud-btn primary full-width" onclick={submitCreateTask} disabled={creatingTask}>
						{creatingTask ? 'CREATING...' : 'CREATE TASK'}
					</button>
				</div>
			{/if}

			{#if loading && !backlog.length}
				<div class="empty-msg font-hud">LOADING BACKLOG...</div>
			{:else if !backlog.length}
				<div class="empty-msg font-hud">BACKLOG EMPTY</div>
			{:else}
				<div class="task-list">
					{#each backlog as task}
						<div
							class="task-card"
							draggable="true"
							ondragstart={(e) => onDragStart(e, task.taskId)}
							role="listitem"
						>
							<div class="task-header">
								<span class="task-title">{task.title}</span>
								<div class="task-header-right">
									<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
									<button class="act-btn" onclick={(e) => { e.stopPropagation(); openEditTask(task); }}>EDIT</button>
								</div>
							</div>
							<div class="task-meta">
								<span class="meta-item">◉ {userDisplayName(task.assigneeId)}</span>
								{#if task.storyPoints > 0}
									<span class="meta-item pts">{task.storyPoints} pts</span>
								{/if}
								{#if task.dueDate}
									<span class="meta-item due">due {task.dueDate}</span>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Right: Sprints -->
		<section class="sprint-col">
			<div class="section-header">
				<h2 class="section-title font-hud">SPRINTS</h2>
				{#if $canManageSprints}
					<button class="hud-btn small" onclick={openSprintCreate}>+ NEW SPRINT</button>
				{/if}
			</div>

			{#if showSprintForm}
				<div class="create-form">
					<div class="form-title font-hud">{editingSprint ? 'EDIT SPRINT' : 'NEW SPRINT'}</div>
					<div class="field">
						<label class="field-label" for="sp-name">NAME *</label>
						<input id="sp-name" class="hud-input" bind:value={sprintName} placeholder="Sprint name..." />
					</div>
					<div class="field">
						<label class="field-label" for="sp-goal">GOAL</label>
						<input id="sp-goal" class="hud-input" bind:value={sprintGoal} placeholder="Sprint goal..." />
					</div>
					<div class="form-row">
						<div class="field">
							<label class="field-label" for="sp-start">START DATE</label>
							<input id="sp-start" class="hud-input" type="date" bind:value={sprintStartDate} />
						</div>
						<div class="field">
							<label class="field-label" for="sp-end">END DATE</label>
							<input id="sp-end" class="hud-input" type="date" bind:value={sprintEndDate} />
						</div>
					</div>
					{#if sprintFormError}
						<div class="form-error">{sprintFormError}</div>
					{/if}
					<div class="form-row">
						<button class="hud-btn full-width" onclick={() => showSprintForm = false}>CANCEL</button>
						<button class="hud-btn primary full-width" onclick={submitSprintForm} disabled={savingSprint}>
							{savingSprint ? 'SAVING...' : 'SAVE'}
						</button>
					</div>
				</div>
			{/if}

			{#if !sprints.length && !loading}
				<div class="empty-msg font-hud">NO SPRINTS</div>
			{:else}
				<div class="sprint-list">
					{#each sprints as sp}
						<div
							class="sprint-card"
							class:active={sp.status === 'SPRINT_STATUS_ACTIVE'}
							class:closed={sp.status === 'SPRINT_STATUS_CLOSED'}
							ondragover={onDragOver}
							ondrop={(e) => onDropSprint(e, sp.sprintId)}
							role="region"
							aria-label="Sprint {sp.name}"
						>
							<div class="sprint-header">
								<span class="sprint-name font-hud">{sp.name}</span>
								<span class="sprint-status-badge" class:status-active={sp.status === 'SPRINT_STATUS_ACTIVE'} class:status-closed={sp.status === 'SPRINT_STATUS_CLOSED'}>
									{sp.status === 'SPRINT_STATUS_ACTIVE' ? 'ACTIVE' : 'CLOSED'}
								</span>
							</div>
							{#if sp.goal}
								<div class="sprint-goal">{sp.goal}</div>
							{/if}
							<div class="sprint-dates">
								{sp.startDate || '—'} → {sp.endDate || '—'}
							</div>
							{#if $canManageSprints && sp.status === 'SPRINT_STATUS_ACTIVE'}
								<div class="sprint-actions">
									<button class="act-btn" onclick={() => openSprintEdit(sp)}>EDIT</button>
									<button class="act-btn danger" onclick={() => closeSprint(sp)}>CLOSE</button>
									<button class="act-btn danger" onclick={() => deleteSprint(sp)}>DELETE</button>
								</div>
							{:else if $canManageSprints}
								<div class="sprint-actions">
									<button class="act-btn danger" onclick={() => deleteSprint(sp)}>DELETE</button>
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</section>
	</div>
</div>

{#if editingTask}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div class="modal-overlay" onclick={() => editingTask = null}>
		<div class="modal-panel" onclick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
			<div class="modal-header">
				<span class="form-title font-hud">EDIT TASK</span>
				<button class="act-btn" onclick={() => editingTask = null}>✕</button>
			</div>
			<div class="field">
				<label class="field-label" for="et-title">TITLE *</label>
				<input id="et-title" class="hud-input" bind:value={editTitle} placeholder="Task title..." />
			</div>
			<div class="field">
				<label class="field-label" for="et-desc">DESCRIPTION</label>
				<textarea id="et-desc" class="hud-input" bind:value={editDescription} rows="2" placeholder="Details..."></textarea>
			</div>
			<div class="form-row">
				<div class="field">
					<label class="field-label" for="et-assignee">ASSIGNEE *</label>
					<select id="et-assignee" class="hud-input" bind:value={editAssigneeId}>
						<option value="">— select —</option>
						{#each userList as u}
							<option value={u.id}>{u.displayName || u.username}</option>
						{/each}
					</select>
				</div>
				<div class="field">
					<label class="field-label" for="et-priority">PRIORITY</label>
					<select id="et-priority" class="hud-input" bind:value={editPriority}>
						<option value="TASK_PRIORITY_CRITICAL">CRITICAL</option>
						<option value="TASK_PRIORITY_HIGH">HIGH</option>
						<option value="TASK_PRIORITY_MEDIUM">MEDIUM</option>
						<option value="TASK_PRIORITY_LOW">LOW</option>
					</select>
				</div>
			</div>
			<div class="form-row">
				<div class="field">
					<label class="field-label" for="et-pts">STORY POINTS</label>
					<input id="et-pts" class="hud-input" type="number" min="0" bind:value={editStoryPoints} />
				</div>
				<div class="field">
					<label class="field-label" for="et-due">DUE DATE</label>
					<input id="et-due" class="hud-input" type="date" bind:value={editDueDate} />
				</div>
			</div>
			<div class="field">
				<label class="field-label" for="et-sprint">SPRINT</label>
				<select id="et-sprint" class="hud-input" bind:value={editSprintId}>
					<option value="">— backlog (no sprint) —</option>
					{#each sprints.filter(s => s.status === 'SPRINT_STATUS_ACTIVE') as sp}
						<option value={sp.sprintId}>{sp.name}</option>
					{/each}
				</select>
			</div>
			{#if editTaskError}
				<div class="form-error">{editTaskError}</div>
			{/if}
			<div class="form-row">
				<button class="hud-btn full-width" onclick={() => editingTask = null}>CANCEL</button>
				<button class="hud-btn primary full-width" onclick={submitEditTask} disabled={savingTask}>
					{savingTask ? 'SAVING...' : 'SAVE CHANGES'}
				</button>
			</div>
		</div>
	</div>
{/if}

<style>
	.backlog-page { width: 100%; }
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 24px; gap: 16px; }
	.page-title { font-size: 20px; font-weight: 900; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0 0 4px; }
	.page-sub { font-size: 11px; color: var(--hud-muted); margin: 0; }
	.header-actions { display: flex; gap: 8px; flex-shrink: 0; }
	.error-banner { background: #1a0505; border: 1px solid #ef444466; color: #ef4444; padding: 10px 14px; font-size: 12px; margin-bottom: 20px; }

	.two-col { display: grid; grid-template-columns: 1fr; gap: 24px; }
	@media (min-width: 900px) {
		.two-col { grid-template-columns: 3fr 2fr; align-items: start; }
	}

	.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 6px; }
	.section-title { font-size: 11px; letter-spacing: 0.2em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0; }
	.count { color: var(--hud-muted); }
	.hud-btn.small { font-size: 9px; padding: 4px 10px; }
	.hud-btn.primary { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.hud-btn.primary:hover { background: #00d4ff18; }
	.full-width { width: 100%; }

	/* Create forms */
	.create-form { background: #0a0e18; border: 1px solid var(--hud-dim); padding: 16px; margin-bottom: 16px; }
	.form-title { font-size: 10px; letter-spacing: 0.2em; color: var(--hud-cyan); margin-bottom: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
	.field-label { font-size: 9px; letter-spacing: 0.15em; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
	.form-error { color: #ef4444; font-size: 11px; margin-bottom: 8px; }

	/* Task list */
	.task-list { display: flex; flex-direction: column; gap: 8px; }
	.task-card {
		background: #0d1117;
		border: 1px solid var(--hud-dim);
		padding: 12px 14px;
		cursor: grab;
		clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.task-card:hover { border-color: var(--hud-cyan); }
	.task-card:active { cursor: grabbing; }
	.task-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 6px; }
	.task-header-right { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
	.task-title { font-size: 12px; color: var(--hud-text); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.priority-badge { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 6px; border: 1px solid; flex-shrink: 0; }
	.priority-critical { color: #ef4444; border-color: #ef444466; }
	.priority-high { color: #f97316; border-color: #f9731666; }
	.priority-medium { color: var(--hud-cyan); border-color: #00d4ff44; }
	.priority-low { color: #6b7280; border-color: #37415166; }
	.task-meta { display: flex; gap: 10px; flex-wrap: wrap; }
	.meta-item { font-size: 10px; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.pts { color: #a78bfa; }
	.due { color: #f59e0b; }

	/* Sprint list */
	.sprint-list { display: flex; flex-direction: column; gap: 12px; }
	.sprint-card {
		background: #0d1117;
		border: 1px solid var(--hud-dim);
		padding: 14px;
		transition: border-color 0.15s, background 0.15s;
		clip-path: polygon(8px 0%, 100% 0%, calc(100% - 8px) 100%, 0% 100%);
	}
	.sprint-card.active { border-color: #00d4ff44; }
	.sprint-card:hover { border-color: var(--hud-cyan); background: #0d1117cc; }
	.sprint-card.closed { opacity: 0.6; }
	.sprint-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; gap: 8px; }
	.sprint-name { font-size: 12px; color: var(--hud-text); flex: 1; }
	.sprint-status-badge { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 6px; border: 1px solid; flex-shrink: 0; }
	.status-active { color: #22c55e; border-color: #22c55e44; }
	.status-closed { color: #6b7280; border-color: #37415166; }
	.sprint-goal { font-size: 11px; color: var(--hud-muted); margin-bottom: 6px; }
	.sprint-dates { font-size: 10px; color: var(--hud-muted); font-family: var(--font-hud, monospace); margin-bottom: 8px; }
	.sprint-actions { display: flex; gap: 6px; }
	.act-btn { font-family: var(--font-hud, monospace); font-size: 9px; letter-spacing: 0.1em; padding: 3px 8px; border: 1px solid var(--hud-dim); background: transparent; color: var(--hud-muted); cursor: pointer; transition: all 0.15s; }
	.act-btn:hover { border-color: var(--hud-cyan); color: var(--hud-cyan); }
	.act-btn.danger:hover { border-color: #ef444466; color: #ef4444; }

	.empty-msg { font-size: 12px; letter-spacing: 0.15em; color: var(--hud-muted); padding: 24px 0; text-align: center; }

	/* Edit modal */
	.modal-overlay {
		position: fixed; inset: 0; background: #00000099; z-index: 100;
		display: flex; align-items: center; justify-content: center; padding: 20px;
	}
	.modal-panel {
		background: #0a0e18; border: 1px solid var(--hud-cyan); padding: 24px;
		width: 100%; max-width: 540px; max-height: 90vh; overflow-y: auto;
		clip-path: polygon(10px 0%, 100% 0%, calc(100% - 10px) 100%, 0% 100%);
	}
	.modal-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
</style>
