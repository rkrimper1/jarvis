<script lang="ts">
	import { tasks, users, type Task, type Sprint, type User, type TaskStatus, type TaskPriority, type TaskType } from '$lib/api/client';
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
	let backlogTasks = $state<Task[]>([]);
	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');
	let draggingTaskId = $state('');

	// All tasks available as parent references (backlog + current sprint, deduped)
	const parentOptions = $derived(
		[...backlogTasks, ...taskList].filter(
			(t, i, arr) => arr.findIndex(x => x.taskId === t.taskId) === i
		)
	);

	let selectedTask = $state<Task | null>(null);
	let didDrag = $state(false);

	// Edit mode state
	let editMode = $state(false);
	let editTitle = $state('');
	let editDescription = $state('');
	let editAssigneeId = $state('');
	let editPriority = $state<TaskPriority>('TASK_PRIORITY_MEDIUM');
	let editTaskType = $state<TaskType>('TASK_TYPE_TASK');
	let editParentId = $state('');
	let editStoryPoints = $state(0);
	let editDueDate = $state('');
	let editError = $state('');
	let saving = $state(false);

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
			const [sp, ul, bl] = await Promise.all([tasks.listSprints(), users.list(), tasks.listBacklog()]);
			sprints = sp.sprints ?? [];
			userList = ul.users ?? [];
			backlogTasks = bl.tasks ?? [];
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

	function openTask(task: Task) {
		selectedTask = task;
		editMode = false;
		editError = '';
	}

	function closeTask() {
		selectedTask = null;
		editMode = false;
		editError = '';
	}

	function enterEditMode() {
		if (!selectedTask) return;
		editTitle = selectedTask.title;
		editDescription = selectedTask.description ?? '';
		editAssigneeId = selectedTask.assigneeId;
		editPriority = selectedTask.priority;
		editTaskType = selectedTask.taskType ?? 'TASK_TYPE_TASK';
		editParentId = selectedTask.parentId ?? '';
		editStoryPoints = selectedTask.storyPoints ?? 0;
		editDueDate = selectedTask.dueDate ?? '';
		editError = '';
		editMode = true;
	}

	function cancelEdit() {
		editMode = false;
		editError = '';
	}

	async function submitEdit() {
		if (!selectedTask) return;
		if (!editTitle.trim()) { editError = 'Title is required.'; return; }
		if (!editAssigneeId) { editError = 'Assignee is required.'; return; }
		editError = '';
		saving = true;
		try {
			const res = await tasks.updateTask(selectedTask.taskId, {
				title: editTitle.trim(),
				description: editDescription.trim(),
				assigneeId: editAssigneeId,
				priority: editPriority,
				taskType: editTaskType,
				parentId: editParentId || undefined,
				storyPoints: editStoryPoints,
				dueDate: editDueDate
			});
			// Update the task in place
			selectedTask = res.task;
			taskList = taskList.map(t => t.taskId === res.task.taskId ? res.task : t);
			editMode = false;
		} catch (e) {
			editError = e instanceof Error ? e.message : String(e);
		} finally {
			saving = false;
		}
	}

	function onModalKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			if (editMode) cancelEdit();
			else closeTask();
		}
	}

	function statusLabel(s: TaskStatus): string {
		return s.replace('TASK_STATUS_', '').replace('_', ' ');
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

	function onDragStart(e: DragEvent, taskId: string) {
		didDrag = true;
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
									role="button"
									tabindex="0"
									ondragstart={(e) => onDragStart(e, task.taskId)}
									ondragend={() => { draggingTaskId = ''; }}
									onmousedown={() => { didDrag = false; }}
									onclick={() => { if (!didDrag) openTask(task); }}
									onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') openTask(task); }}
								>
									<div class="task-top">
										<span class="task-title">{#if task.displayId}<span class="task-id">{displayId(task.displayId)}:</span> {/if}{task.title}</span>
										<div class="task-badges">
											<span class="type-badge {taskTypeClass(task.taskType)}">{taskTypeLabel(task.taskType)}</span>
											<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
										</div>
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
											<span class="task-title">{#if task.displayId}<span class="task-id">{displayId(task.displayId)}:</span> {/if}{task.title}</span>
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
									role="button"
									tabindex="0"
									ondragstart={(e) => onDragStart(e, task.taskId)}
									ondragend={() => { draggingTaskId = ''; }}
									onmousedown={() => { didDrag = false; }}
									onclick={() => { if (!didDrag) openTask(task); }}
									onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') openTask(task); }}
								>
									<div class="task-top">
										<span class="task-title">{#if task.displayId}<span class="task-id">{displayId(task.displayId)}:</span> {/if}{task.title}</span>
										<div class="task-badges">
											<span class="type-badge {taskTypeClass(task.taskType)}">{taskTypeLabel(task.taskType)}</span>
											<span class="priority-badge {priorityClass(task.priority)}">{priorityLabel(task.priority)}</span>
										</div>
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

{#if selectedTask}
	<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
	<div class="modal-backdrop" onclick={() => { if (!editMode) closeTask(); }} onkeydown={onModalKeydown} role="dialog" aria-modal="true" tabindex="0">
		<div class="modal-panel" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="document">
			<div class="modal-header font-hud">
				<span class="modal-title">{editMode ? 'EDIT TASK' : 'TASK DETAIL'}</span>
				<div class="modal-header-actions">
					{#if !editMode}
						<button class="hud-btn small" onclick={enterEditMode}>EDIT</button>
					{/if}
					<button class="modal-close" onclick={closeTask}>✕</button>
				</div>
			</div>

			{#if editMode}
				<!-- ── Edit mode ─────────────────────────────────────── -->
				<div class="modal-body">
					{#if selectedTask.displayId}
						<div class="detail-display-id">{displayId(selectedTask.displayId)}</div>
					{/if}

					<div class="edit-field">
						<label class="edit-label" for="me-title">TITLE *</label>
						<input id="me-title" class="hud-input" bind:value={editTitle} />
					</div>
					<div class="edit-field">
						<label class="edit-label" for="me-desc">DESCRIPTION</label>
						<textarea id="me-desc" class="hud-input" bind:value={editDescription} rows="3"></textarea>
					</div>
					<div class="edit-row">
						<div class="edit-field">
							<label class="edit-label" for="me-type">TYPE</label>
							<select id="me-type" class="hud-input" bind:value={editTaskType}>
								<option value="TASK_TYPE_TASK">Task</option>
								<option value="TASK_TYPE_EPIC">Epic</option>
								<option value="TASK_TYPE_STORY">Story</option>
								<option value="TASK_TYPE_BUG">Bug</option>
								<option value="TASK_TYPE_SUBTASK">Sub-task</option>
							</select>
						</div>
						<div class="edit-field">
							<label class="edit-label" for="me-priority">PRIORITY</label>
							<select id="me-priority" class="hud-input" bind:value={editPriority}>
								<option value="TASK_PRIORITY_CRITICAL">CRITICAL</option>
								<option value="TASK_PRIORITY_HIGH">HIGH</option>
								<option value="TASK_PRIORITY_MEDIUM">MEDIUM</option>
								<option value="TASK_PRIORITY_LOW">LOW</option>
							</select>
						</div>
					</div>
					<div class="edit-row">
						<div class="edit-field">
							<label class="edit-label" for="me-assignee">ASSIGNEE *</label>
							<select id="me-assignee" class="hud-input" bind:value={editAssigneeId}>
								<option value="">— select —</option>
								{#each userList as u}
									<option value={u.id}>{u.displayName || u.username}</option>
								{/each}
							</select>
						</div>
						<div class="edit-field">
							<label class="edit-label" for="me-pts">STORY POINTS</label>
							<input id="me-pts" class="hud-input" type="number" min="0" bind:value={editStoryPoints} />
						</div>
					</div>
					<div class="edit-row">
						<div class="edit-field">
							<label class="edit-label" for="me-due">DUE DATE</label>
							<input id="me-due" class="hud-input" type="date" bind:value={editDueDate} />
						</div>
						<div class="edit-field">
							<label class="edit-label" for="me-parent">PARENT TASK</label>
							<select id="me-parent" class="hud-input" bind:value={editParentId}>
								<option value="">— none —</option>
								{#each parentOptions.filter(t => t.taskId !== selectedTask?.taskId) as t}
									<option value={t.taskId}>{displayId(t.displayId) ? `${displayId(t.displayId)}: ` : ''}{t.title}</option>
								{/each}
							</select>
						</div>
					</div>

					{#if editError}
						<div class="edit-error">{editError}</div>
					{/if}

					<div class="edit-actions">
						<button class="hud-btn" onclick={cancelEdit} disabled={saving}>CANCEL</button>
						<button class="hud-btn primary" onclick={submitEdit} disabled={saving}>
							{saving ? 'SAVING...' : 'SAVE CHANGES'}
						</button>
					</div>
				</div>

			{:else}
				<!-- ── View mode ─────────────────────────────────────── -->
				<div class="modal-body">
					{#if selectedTask.displayId}
						<div class="detail-display-id">{displayId(selectedTask.displayId)}</div>
					{/if}
					<div class="detail-title">{selectedTask.title}</div>

					<div class="detail-badges">
						<span class="type-badge {taskTypeClass(selectedTask.taskType)}">{taskTypeLabel(selectedTask.taskType)}</span>
						<span class="priority-badge {priorityClass(selectedTask.priority)}">{priorityLabel(selectedTask.priority)}</span>
						<span class="status-badge">{statusLabel(selectedTask.status)}</span>
						{#if selectedTask.storyPoints > 0}
							<span class="pts-badge">{selectedTask.storyPoints} PT</span>
						{/if}
					</div>

					{#if selectedTask.description}
						<div class="detail-section">
							<div class="detail-label">DESCRIPTION</div>
							<div class="detail-desc">{selectedTask.description}</div>
						</div>
					{/if}

					<div class="detail-grid">
						{#if selectedTask.parentId}
							<div class="detail-field">
								<div class="detail-label">PARENT</div>
								<div class="detail-value">{userDisplayName(selectedTask.parentId) || selectedTask.parentId}</div>
							</div>
						{/if}
						<div class="detail-field">
							<div class="detail-label">ASSIGNEE</div>
							<div class="detail-value">{userDisplayName(selectedTask.assigneeId) || '—'}</div>
						</div>
						<div class="detail-field">
							<div class="detail-label">REPORTER</div>
							<div class="detail-value">{userDisplayName(selectedTask.reporterId) || '—'}</div>
						</div>
						{#if selectedTask.dueDate}
							<div class="detail-field">
								<div class="detail-label">DUE DATE</div>
								<div class="detail-value">{formatDate(selectedTask.dueDate)}</div>
							</div>
						{/if}
						{#if selectedTask.completedById}
							<div class="detail-field">
								<div class="detail-label">COMPLETED BY</div>
								<div class="detail-value">{userDisplayName(selectedTask.completedById)}</div>
							</div>
						{/if}
						{#if selectedTask.completedAt}
							<div class="detail-field">
								<div class="detail-label">COMPLETED</div>
								<div class="detail-value">{formatDate(selectedTask.completedAt)}</div>
							</div>
						{/if}
						<div class="detail-field">
							<div class="detail-label">CREATED</div>
							<div class="detail-value">{formatDate(selectedTask.createdAt)}</div>
						</div>
						<div class="detail-field">
							<div class="detail-label">ID</div>
							<div class="detail-value id-value">{selectedTask.taskId}</div>
						</div>
					</div>
				</div>
			{/if}
		</div>
	</div>
{/if}

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

	/* ── Task detail modal ─────────────────────────────────────────── */
	.modal-backdrop {
		position: fixed; inset: 0; z-index: 200;
		background: rgba(0,0,0,0.7);
		display: flex; align-items: center; justify-content: center;
	}
	.modal-panel {
		background: #080c14;
		border: 1px solid var(--hud-cyan);
		box-shadow: 0 0 30px #00d4ff22;
		width: 520px; max-width: 95vw; max-height: 85vh;
		display: flex; flex-direction: column;
		clip-path: polygon(8px 0%, 100% 0%, calc(100% - 8px) 100%, 0% 100%);
	}
	.modal-header {
		display: flex; align-items: center; justify-content: space-between;
		padding: 12px 16px;
		border-bottom: 1px solid var(--hud-dim);
		flex-shrink: 0;
	}
	.modal-title { font-size: 11px; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); }
	.modal-header-actions { display: flex; align-items: center; gap: 8px; }
	.modal-close {
		background: none; border: none; color: var(--hud-muted); cursor: pointer;
		font-size: 14px; line-height: 1; padding: 2px 4px;
	}
	.modal-close:hover { color: var(--hud-cyan); }

	/* edit form inside modal */
	.edit-field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
	.edit-label { font-size: 9px; letter-spacing: 0.15em; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.edit-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
	.edit-error { color: #ef4444; font-size: 11px; margin-bottom: 10px; }
	.edit-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px; }
	.hud-btn.small { font-size: 9px; padding: 4px 10px; }
	.hud-btn.primary { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.hud-btn.primary:hover { background: #00d4ff18; }

	.modal-body { padding: 16px; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; }

	.task-id { color: var(--hud-cyan); opacity: 0.7; font-size: 9px; letter-spacing: 0.05em; }
	.task-badges { display: flex; flex-direction: column; gap: 3px; flex-shrink: 0; }
	.type-badge { font-size: 8px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 5px; border: 1px solid; flex-shrink: 0; }
	.type-task    { color: #94a3b8; border-color: #94a3b844; }
	.type-epic    { color: #a78bfa; border-color: #a78bfa66; }
	.type-story   { color: #34d399; border-color: #34d39966; }
	.type-bug     { color: #f87171; border-color: #f8717166; }
	.type-subtask { color: #60a5fa; border-color: #60a5fa66; }
	.detail-display-id { font-size: 10px; letter-spacing: 0.2em; color: var(--hud-cyan); font-family: var(--font-hud, monospace); margin-bottom: 2px; }
	.detail-title { font-size: 15px; color: var(--hud-text); line-height: 1.4; }

	.detail-badges { display: flex; gap: 8px; flex-wrap: wrap; }
	.status-badge {
		font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.12em;
		padding: 2px 7px; border: 1px solid #00d4ff44; color: var(--hud-muted);
	}
	.pts-badge {
		font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em;
		padding: 2px 7px; border: 1px solid #a78bfa44; color: #a78bfa;
	}

	.detail-section { display: flex; flex-direction: column; gap: 6px; }
	.detail-label { font-size: 9px; letter-spacing: 0.2em; color: var(--hud-cyan); font-family: var(--font-hud, monospace); }
	.detail-desc { font-size: 12px; color: var(--hud-muted); line-height: 1.5; white-space: pre-wrap; }

	.detail-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px 16px; }
	.detail-field { display: flex; flex-direction: column; gap: 4px; }
	.detail-value { font-size: 11px; color: var(--hud-text); }
	.id-value { font-size: 9px; color: var(--hud-dim); word-break: break-all; }
</style>
