<script lang="ts">
	import { tasks, users, type Task, type Sprint, type User, type TaskPriority, type TaskType } from '$lib/api/client';
	import { userId } from '$lib/stores/auth';

	let backlog = $state<Task[]>([]);
	let allTasks = $state<Task[]>([]);
	let sprints = $state<Sprint[]>([]);
	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');

	const filteredBacklog = $derived(
		backlog.filter(t => t.taskType !== 'TASK_TYPE_EPIC' && t.taskType !== 'TASK_TYPE_STORY')
	);

	// Create task form
	let showCreateTask = $state(false);
	let newTitle = $state('');
	let newDescription = $state('');
	let newAssigneeId = $state('');
	let newPriority = $state<TaskPriority>('TASK_PRIORITY_MEDIUM');
	let newTaskType = $state<TaskType>('TASK_TYPE_TASK');
	let newParentId = $state('');
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
	let editTaskType = $state<TaskType>('TASK_TYPE_TASK');
	let editParentId = $state('');
	let editStoryPoints = $state(0);
	let editDueDate = $state('');
	let editSprintId = $state('');
	let editTaskError = $state('');
	let savingTask = $state(false);


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

	function displayId(id: number): string {
		return id > 0 ? `JARVIS-${String(id).padStart(4, '0')}` : '';
	}

	function taskTypeLabel(t: TaskType): string {
		switch (t) {
			case 'TASK_TYPE_EPIC':    return 'EPIC';
			case 'TASK_TYPE_STORY':   return 'STORY';
			case 'TASK_TYPE_BUG':     return 'BUG';
			case 'TASK_TYPE_SUBTASK': return 'SUBTASK';
			default:                   return 'TASK';
		}
	}

	function taskTypeClass(t: TaskType): string {
		switch (t) {
			case 'TASK_TYPE_EPIC':    return 'type-epic';
			case 'TASK_TYPE_STORY':   return 'type-story';
			case 'TASK_TYPE_BUG':     return 'type-bug';
			case 'TASK_TYPE_SUBTASK': return 'type-subtask';
			default:                   return 'type-task';
		}
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const [bl, sp, ul, all] = await Promise.all([
				tasks.listBacklog(),
				tasks.listSprints(),
				users.list(),
				tasks.listAllTasks()
			]);
			backlog = bl.tasks ?? [];
			sprints = sp.sprints ?? [];
			userList = ul.users ?? [];
			allTasks = all.tasks ?? [];
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
				taskType: newTaskType,
				parentId: newParentId || undefined,
				storyPoints: newStoryPoints,
				dueDate: newDueDate,
				sprintId: newSprintId || undefined
			});
			newTitle = ''; newDescription = ''; newAssigneeId = ''; newPriority = 'TASK_PRIORITY_MEDIUM';
			newTaskType = 'TASK_TYPE_TASK'; newParentId = '';
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
		editTaskType = task.taskType ?? 'TASK_TYPE_TASK';
		editParentId = task.parentId ?? '';
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
				taskType: editTaskType,
				parentId: editParentId || undefined,
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

	<section class="backlog-col">
			<div class="section-header">
				<h2 class="section-title font-hud">BACKLOG <span class="count">({filteredBacklog.length})</span></h2>
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
							<label class="field-label" for="ct-type">TYPE</label>
							<select id="ct-type" class="hud-input" bind:value={newTaskType}>
								<option value="TASK_TYPE_TASK">Task</option>
								<option value="TASK_TYPE_EPIC">Epic</option>
								<option value="TASK_TYPE_STORY">Story</option>
								<option value="TASK_TYPE_BUG">Bug</option>
								<option value="TASK_TYPE_SUBTASK">Sub-task</option>
							</select>
						</div>
						<div class="field">
							<label class="field-label" for="ct-assignee">ASSIGNEE *</label>
							<select id="ct-assignee" class="hud-input" bind:value={newAssigneeId}>
								<option value="">— select —</option>
								{#each userList as u}
									<option value={u.id}>{u.displayName || u.username}</option>
								{/each}
							</select>
						</div>
					</div>
					<div class="form-row">
						<div class="field">
							<label class="field-label" for="ct-priority">PRIORITY</label>
							<select id="ct-priority" class="hud-input" bind:value={newPriority}>
								<option value="TASK_PRIORITY_CRITICAL">CRITICAL</option>
								<option value="TASK_PRIORITY_HIGH">HIGH</option>
								<option value="TASK_PRIORITY_MEDIUM">MEDIUM</option>
								<option value="TASK_PRIORITY_LOW">LOW</option>
							</select>
						</div>
						<div class="field">
							<label class="field-label" for="ct-parent">PARENT TASK</label>
							<select id="ct-parent" class="hud-input" bind:value={newParentId}>
								<option value="">— none —</option>
								{#each allTasks as t}
									<option value={t.taskId}>{displayId(t.displayId) ? `${displayId(t.displayId)}: ` : ''}{t.title}</option>
								{/each}
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

			{#if loading && !filteredBacklog.length}
				<div class="empty-msg font-hud">LOADING BACKLOG...</div>
			{:else if !filteredBacklog.length}
				<div class="empty-msg font-hud">BACKLOG EMPTY</div>
			{:else}
				<div class="task-list">
					{#each filteredBacklog as task}
						<div
							class="task-card"
							role="listitem"
						>
							<div class="task-header">
								<span class="task-title">{#if task.displayId}<span class="task-id">{displayId(task.displayId)}:</span> {/if}{task.title}</span>
								<div class="task-header-right">
									<span class="type-badge {taskTypeClass(task.taskType)}">{taskTypeLabel(task.taskType)}</span>
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
</div>

{#if editingTask}
	<div class="modal-overlay" onclick={() => editingTask = null} onkeydown={(e) => { if (e.key === 'Escape') editingTask = null; }} role="presentation">
		<div class="modal-panel" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="dialog" aria-modal="true" tabindex="0">
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
					<label class="field-label" for="et-type">TYPE</label>
					<select id="et-type" class="hud-input" bind:value={editTaskType}>
						<option value="TASK_TYPE_TASK">Task</option>
						<option value="TASK_TYPE_EPIC">Epic</option>
						<option value="TASK_TYPE_STORY">Story</option>
						<option value="TASK_TYPE_BUG">Bug</option>
						<option value="TASK_TYPE_SUBTASK">Sub-task</option>
					</select>
				</div>
				<div class="field">
					<label class="field-label" for="et-assignee">ASSIGNEE *</label>
					<select id="et-assignee" class="hud-input" bind:value={editAssigneeId}>
						<option value="">— select —</option>
						{#each userList as u}
							<option value={u.id}>{u.displayName || u.username}</option>
						{/each}
					</select>
				</div>
			</div>
			<div class="form-row">
				<div class="field">
					<label class="field-label" for="et-priority">PRIORITY</label>
					<select id="et-priority" class="hud-input" bind:value={editPriority}>
						<option value="TASK_PRIORITY_CRITICAL">CRITICAL</option>
						<option value="TASK_PRIORITY_HIGH">HIGH</option>
						<option value="TASK_PRIORITY_MEDIUM">MEDIUM</option>
						<option value="TASK_PRIORITY_LOW">LOW</option>
					</select>
				</div>
				<div class="field">
					<label class="field-label" for="et-parent">PARENT TASK</label>
					<select id="et-parent" class="hud-input" bind:value={editParentId}>
						<option value="">— none —</option>
						{#each allTasks.filter(t => t.taskId !== editingTask?.taskId) as t}
							<option value={t.taskId}>{displayId(t.displayId) ? `${displayId(t.displayId)}: ` : ''}{t.title}</option>
						{/each}
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

	.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 6px; gap: 8px; }
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
		clip-path: polygon(6px 0%, 100% 0%, calc(100% - 6px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.task-card:hover { border-color: var(--hud-cyan); }
	.task-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 6px; }
	.task-header-right { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
	.task-id { color: var(--hud-cyan); opacity: 0.7; font-size: 9px; letter-spacing: 0.05em; }
	.task-title { font-size: 12px; color: var(--hud-text); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.type-badge { font-size: 8px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 5px; border: 1px solid; flex-shrink: 0; }
	.type-task    { color: #94a3b8; border-color: #94a3b844; }
	.type-epic    { color: #a78bfa; border-color: #a78bfa66; }
	.type-story   { color: #34d399; border-color: #34d39966; }
	.type-bug     { color: #f87171; border-color: #f8717166; }
	.type-subtask { color: #60a5fa; border-color: #60a5fa66; }
	.priority-badge { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 6px; border: 1px solid; flex-shrink: 0; }
	.priority-critical { color: #ef4444; border-color: #ef444466; }
	.priority-high { color: #f97316; border-color: #f9731666; }
	.priority-medium { color: var(--hud-cyan); border-color: #00d4ff44; }
	.priority-low { color: #6b7280; border-color: #37415166; }
	.task-meta { display: flex; gap: 10px; flex-wrap: wrap; }
	.meta-item { font-size: 10px; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.pts { color: #a78bfa; }
	.due { color: #f59e0b; }
	.task-desc { font-size: 10px; color: var(--hud-muted); margin-top: 6px; line-height: 1.4; white-space: pre-wrap; }
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
