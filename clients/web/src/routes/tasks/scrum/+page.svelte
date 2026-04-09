<script lang="ts">
	import { tasks, users, type Task, type Sprint, type User, type TaskPriority, type TaskType, type TaskStatus } from '$lib/api/client';
	import { canManageSprints } from '$lib/stores/auth';

	let allTasks = $state<Task[]>([]);
	let sprints = $state<Sprint[]>([]);
	let userList = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');
	let activeTab = $state<'sprints' | 'stories' | 'epics'>('sprints');
	let expandedIds = $state<Set<string>>(new Set());

	const ACTIVE_STATUSES: TaskStatus[] = [
		'TASK_STATUS_UNASSIGNED', 'TASK_STATUS_ASSIGNED', 'TASK_STATUS_IN_PROGRESS',
		'TASK_STATUS_TESTING', 'TASK_STATUS_REVIEW'
	];

	const stories = $derived(
		allTasks.filter(t => t.taskType === 'TASK_TYPE_STORY' && ACTIVE_STATUSES.includes(t.status))
	);
	const epics = $derived(
		allTasks.filter(t => t.taskType === 'TASK_TYPE_EPIC' && ACTIVE_STATUSES.includes(t.status))
	);

	// Sprint form
	let showSprintForm = $state(false);
	let editingSprint = $state<Sprint | null>(null);
	let sprintName = $state('');
	let sprintGoal = $state('');
	let sprintStartDate = $state('');
	let sprintEndDate = $state('');
	let sprintFormError = $state('');
	let savingSprint = $state(false);

	function userDisplayName(id: string): string {
		const u = userList.find(u => u.id === id);
		return u ? (u.displayName || u.username) : id;
	}

	function statusLabel(s: TaskStatus): string {
		switch (s) {
			case 'TASK_STATUS_UNASSIGNED':  return 'UNASSIGNED';
			case 'TASK_STATUS_ASSIGNED':    return 'ASSIGNED';
			case 'TASK_STATUS_IN_PROGRESS': return 'IN PROGRESS';
			case 'TASK_STATUS_TESTING':     return 'TESTING';
			case 'TASK_STATUS_REVIEW':      return 'REVIEW';
			case 'TASK_STATUS_COMPLETED':   return 'DONE';
			default:                         return s;
		}
	}

	function statusClass(s: TaskStatus): string {
		switch (s) {
			case 'TASK_STATUS_UNASSIGNED':  return 'status-unassigned';
			case 'TASK_STATUS_ASSIGNED':    return 'status-assigned';
			case 'TASK_STATUS_IN_PROGRESS': return 'status-inprogress';
			case 'TASK_STATUS_TESTING':     return 'status-testing';
			case 'TASK_STATUS_REVIEW':      return 'status-review';
			case 'TASK_STATUS_COMPLETED':   return 'status-done';
			default:                         return '';
		}
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
			case 'TASK_TYPE_SUBTASK': return 'SUB';
			default:                   return 'TASK';
		}
	}

	function sprintTasks(sprintId: string): Task[] {
		return allTasks.filter(t => t.sprintId === sprintId);
	}

	function childTasks(taskId: string): Task[] {
		return allTasks.filter(t => t.parentId === taskId);
	}

	function toggleExpand(id: string) {
		const next = new Set(expandedIds);
		if (next.has(id)) { next.delete(id); } else { next.add(id); }
		expandedIds = next;
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const [sp, ul, all] = await Promise.all([
				tasks.listSprints(),
				users.list(),
				tasks.listAllTasks()
			]);
			sprints = sp.sprints ?? [];
			userList = ul.users ?? [];
			allTasks = all.tasks ?? [];
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		} finally {
			loading = false;
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

	load();
</script>

<div class="scrum-page">
	<div class="page-header">
		<div>
			<h1 class="page-title font-hud">TASK MANAGEMENT · SCRUM</h1>
			<p class="page-sub">Sprints, stories, and epics with their child tasks</p>
		</div>
		<div class="header-actions">
			<button class="hud-btn" onclick={load} disabled={loading}>
				{loading ? 'LOADING...' : 'REFRESH'}
			</button>
		</div>
	</div>

	{#if error}
		<div class="error-banner">{error}</div>
	{/if}

	<div class="tab-row">
		<div class="tab-bar">
			<button class="tab-btn" class:tab-active={activeTab === 'sprints'} onclick={() => activeTab = 'sprints'}>
				SPRINTS <span class="tab-count">{sprints.length}</span>
			</button>
			<button class="tab-btn" class:tab-active={activeTab === 'stories'} onclick={() => activeTab = 'stories'}>
				STORIES <span class="tab-count">{stories.length}</span>
			</button>
			<button class="tab-btn" class:tab-active={activeTab === 'epics'} onclick={() => activeTab = 'epics'}>
				EPICS <span class="tab-count">{epics.length}</span>
			</button>
		</div>
		{#if activeTab === 'sprints' && $canManageSprints}
			<button class="hud-btn small" onclick={openSprintCreate}>+ NEW SPRINT</button>
		{/if}
	</div>

	{#if activeTab === 'sprints'}
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
			<div class="parent-list">
				{#each sprints as sp}
					{@const spTasks = sprintTasks(sp.sprintId)}
					{@const isOpen = expandedIds.has(sp.sprintId)}
					<div class="parent-card" class:active={sp.status === 'SPRINT_STATUS_ACTIVE'} class:closed={sp.status === 'SPRINT_STATUS_CLOSED'}>
						<div class="parent-header" onclick={() => toggleExpand(sp.sprintId)} role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && toggleExpand(sp.sprintId)}>
							<span class="expand-icon">{isOpen ? '▾' : '▸'}</span>
							<span class="parent-name font-hud">{sp.name}</span>
							<span class="sprint-status-badge" class:status-active={sp.status === 'SPRINT_STATUS_ACTIVE'} class:status-closed={sp.status === 'SPRINT_STATUS_CLOSED'}>
								{sp.status === 'SPRINT_STATUS_ACTIVE' ? 'ACTIVE' : 'CLOSED'}
							</span>
							<span class="child-count">{spTasks.length} task{spTasks.length !== 1 ? 's' : ''}</span>
							{#if $canManageSprints && sp.status === 'SPRINT_STATUS_ACTIVE'}
								<div class="parent-actions" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="presentation">
									<button class="act-btn" onclick={() => openSprintEdit(sp)}>EDIT</button>
									<button class="act-btn danger" onclick={() => closeSprint(sp)}>CLOSE</button>
									<button class="act-btn danger" onclick={() => deleteSprint(sp)}>DELETE</button>
								</div>
							{:else if $canManageSprints}
								<div class="parent-actions" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="presentation">
									<button class="act-btn danger" onclick={() => deleteSprint(sp)}>DELETE</button>
								</div>
							{/if}
						</div>
						{#if sp.goal}
							<div class="parent-sub">{sp.goal}</div>
						{/if}
						{#if sp.startDate || sp.endDate}
							<div class="parent-dates">{sp.startDate || '—'} → {sp.endDate || '—'}</div>
						{/if}
						{#if isOpen}
							{#if !spTasks.length}
								<div class="children-empty">No tasks in this sprint</div>
							{:else}
								<ul class="child-list">
									{#each spTasks as t}
										<li class="child-item">
											<span class="child-id">{displayId(t.displayId) || '·'}</span>
											<span class="child-title">{t.title}</span>
											<span class="child-type type-{t.taskType?.replace('TASK_TYPE_', '').toLowerCase()}">{taskTypeLabel(t.taskType)}</span>
											<span class="child-priority {priorityClass(t.priority)}">{priorityLabel(t.priority)}</span>
											<span class="child-status {statusClass(t.status)}">{statusLabel(t.status)}</span>
											<span class="child-assignee">◉ {userDisplayName(t.assigneeId)}</span>
										</li>
									{/each}
								</ul>
							{/if}
						{/if}
					</div>
				{/each}
			</div>
		{/if}

	{:else if activeTab === 'stories'}
		{#if !stories.length && !loading}
			<div class="empty-msg font-hud">NO ACTIVE STORIES</div>
		{:else}
			<div class="parent-list">
				{#each stories as story}
					{@const children = childTasks(story.taskId)}
					{@const isOpen = expandedIds.has(story.taskId)}
					<div class="parent-card story-card">
						<div class="parent-header" onclick={() => toggleExpand(story.taskId)} role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && toggleExpand(story.taskId)}>
							<span class="expand-icon">{isOpen ? '▾' : '▸'}</span>
							<span class="parent-name">{#if story.displayId}<span class="parent-id">{displayId(story.displayId)}:</span> {/if}{story.title}</span>
							<span class="child-count">{children.length} child{children.length !== 1 ? 'ren' : ''}</span>
							<span class="child-priority {priorityClass(story.priority)}">{priorityLabel(story.priority)}</span>
							<span class="child-status {statusClass(story.status)}">{statusLabel(story.status)}</span>
						</div>
						{#if story.description}
							<div class="parent-sub">{story.description}</div>
						{/if}
						<div class="parent-meta">
							<span>◉ {userDisplayName(story.assigneeId)}</span>
							{#if story.storyPoints > 0}<span class="pts">{story.storyPoints} pts</span>{/if}
							{#if story.dueDate}<span class="due">due {story.dueDate}</span>{/if}
						</div>
						{#if isOpen}
							{#if !children.length}
								<div class="children-empty">No child tasks</div>
							{:else}
								<ul class="child-list">
									{#each children as t}
										<li class="child-item">
											<span class="child-id">{displayId(t.displayId) || '·'}</span>
											<span class="child-title">{t.title}</span>
											<span class="child-type type-{t.taskType?.replace('TASK_TYPE_', '').toLowerCase()}">{taskTypeLabel(t.taskType)}</span>
											<span class="child-priority {priorityClass(t.priority)}">{priorityLabel(t.priority)}</span>
											<span class="child-status {statusClass(t.status)}">{statusLabel(t.status)}</span>
											<span class="child-assignee">◉ {userDisplayName(t.assigneeId)}</span>
										</li>
									{/each}
								</ul>
							{/if}
						{/if}
					</div>
				{/each}
			</div>
		{/if}

	{:else if activeTab === 'epics'}
		{#if !epics.length && !loading}
			<div class="empty-msg font-hud">NO ACTIVE EPICS</div>
		{:else}
			<div class="parent-list">
				{#each epics as epic}
					{@const children = childTasks(epic.taskId)}
					{@const isOpen = expandedIds.has(epic.taskId)}
					<div class="parent-card epic-card">
						<div class="parent-header" onclick={() => toggleExpand(epic.taskId)} role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && toggleExpand(epic.taskId)}>
							<span class="expand-icon">{isOpen ? '▾' : '▸'}</span>
							<span class="parent-name">{#if epic.displayId}<span class="parent-id">{displayId(epic.displayId)}:</span> {/if}{epic.title}</span>
							<span class="child-count">{children.length} child{children.length !== 1 ? 'ren' : ''}</span>
							<span class="child-priority {priorityClass(epic.priority)}">{priorityLabel(epic.priority)}</span>
							<span class="child-status {statusClass(epic.status)}">{statusLabel(epic.status)}</span>
						</div>
						{#if epic.description}
							<div class="parent-sub">{epic.description}</div>
						{/if}
						<div class="parent-meta">
							<span>◉ {userDisplayName(epic.assigneeId)}</span>
							{#if epic.storyPoints > 0}<span class="pts">{epic.storyPoints} pts</span>{/if}
							{#if epic.dueDate}<span class="due">due {epic.dueDate}</span>{/if}
						</div>
						{#if isOpen}
							{#if !children.length}
								<div class="children-empty">No child tasks</div>
							{:else}
								<ul class="child-list">
									{#each children as t}
										<li class="child-item">
											<span class="child-id">{displayId(t.displayId) || '·'}</span>
											<span class="child-title">{t.title}</span>
											<span class="child-type type-{t.taskType?.replace('TASK_TYPE_', '').toLowerCase()}">{taskTypeLabel(t.taskType)}</span>
											<span class="child-priority {priorityClass(t.priority)}">{priorityLabel(t.priority)}</span>
											<span class="child-status {statusClass(t.status)}">{statusLabel(t.status)}</span>
											<span class="child-assignee">◉ {userDisplayName(t.assigneeId)}</span>
										</li>
									{/each}
								</ul>
							{/if}
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	{/if}
</div>

<style>
	.scrum-page { width: 100%; }
	.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 20px; gap: 16px; }
	.page-title { font-size: 20px; font-weight: 900; letter-spacing: 0.25em; color: var(--hud-cyan); text-shadow: var(--glow-cyan); margin: 0 0 4px; }
	.page-sub { font-size: 11px; color: var(--hud-muted); margin: 0; }
	.header-actions { display: flex; gap: 8px; flex-shrink: 0; }
	.error-banner { background: #1a0505; border: 1px solid #ef444466; color: #ef4444; padding: 10px 14px; font-size: 12px; margin-bottom: 20px; }

	.tab-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; border-bottom: 1px solid var(--hud-dim); padding-bottom: 0; gap: 12px; }
	.tab-bar { display: flex; gap: 0; }
	.tab-btn { font-family: var(--font-hud, monospace); font-size: 10px; letter-spacing: 0.15em; padding: 6px 16px; border: 1px solid var(--hud-dim); border-bottom: none; border-right: none; background: transparent; color: var(--hud-muted); cursor: pointer; transition: all 0.15s; }
	.tab-btn:last-child { border-right: 1px solid var(--hud-dim); }
	.tab-btn:hover { color: var(--hud-cyan); border-color: #00d4ff44; }
	.tab-btn.tab-active { color: var(--hud-cyan); border-color: var(--hud-cyan); background: #00d4ff0d; text-shadow: var(--glow-cyan); }
	.tab-count { font-size: 9px; color: var(--hud-muted); margin-left: 4px; }
	.tab-btn.tab-active .tab-count { color: var(--hud-cyan); }
	.hud-btn.small { font-size: 9px; padding: 4px 10px; }
	.hud-btn.primary { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.hud-btn.primary:hover { background: #00d4ff18; }
	.full-width { width: 100%; }

	/* Sprint form */
	.create-form { background: #0a0e18; border: 1px solid var(--hud-dim); padding: 16px; margin-bottom: 16px; max-width: 540px; }
	.form-title { font-size: 10px; letter-spacing: 0.2em; color: var(--hud-cyan); margin-bottom: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
	.field-label { font-size: 9px; letter-spacing: 0.15em; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.form-row { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
	.form-error { color: #ef4444; font-size: 11px; margin-bottom: 8px; }

	/* Parent list */
	.parent-list { display: flex; flex-direction: column; gap: 10px; }
	.parent-card {
		background: #0d1117;
		border: 1px solid var(--hud-dim);
		clip-path: polygon(8px 0%, 100% 0%, calc(100% - 8px) 100%, 0% 100%);
		transition: border-color 0.15s;
	}
	.parent-card:hover { border-color: #00d4ff44; }
	.parent-card.active { border-color: #00d4ff33; }
	.parent-card.closed { opacity: 0.65; }
	.story-card { border-color: #34d39933; }
	.story-card:hover { border-color: #34d39966; }
	.epic-card { border-color: #a78bfa33; }
	.epic-card:hover { border-color: #a78bfa66; }

	.parent-header {
		display: flex; align-items: center; gap: 8px; padding: 12px 14px;
		cursor: pointer; user-select: none;
	}
	.expand-icon { color: var(--hud-muted); font-size: 10px; flex-shrink: 0; width: 12px; }
	.parent-name { font-size: 13px; color: var(--hud-text); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-hud, monospace); letter-spacing: 0.05em; }
	.parent-id { color: var(--hud-cyan); opacity: 0.7; font-size: 10px; }
	.sprint-status-badge { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 2px 6px; border: 1px solid; flex-shrink: 0; }
	.status-active { color: #22c55e; border-color: #22c55e44; }
	.status-closed { color: #6b7280; border-color: #37415166; }
	.child-count { font-size: 9px; color: var(--hud-muted); font-family: var(--font-hud, monospace); flex-shrink: 0; }
	.parent-actions { display: flex; gap: 4px; flex-shrink: 0; }
	.act-btn { font-family: var(--font-hud, monospace); font-size: 9px; letter-spacing: 0.1em; padding: 3px 8px; border: 1px solid var(--hud-dim); background: transparent; color: var(--hud-muted); cursor: pointer; transition: all 0.15s; }
	.act-btn:hover { border-color: var(--hud-cyan); color: var(--hud-cyan); }
	.act-btn.danger:hover { border-color: #ef444466; color: #ef4444; }

	.parent-sub { font-size: 11px; color: var(--hud-muted); padding: 0 14px 8px 34px; line-height: 1.4; }
	.parent-dates { font-size: 10px; color: var(--hud-muted); font-family: var(--font-hud, monospace); padding: 0 14px 8px 34px; }
	.parent-meta { display: flex; gap: 10px; padding: 0 14px 10px 34px; font-size: 10px; color: var(--hud-muted); font-family: var(--font-hud, monospace); }
	.pts { color: #a78bfa; }
	.due { color: #f59e0b; }

	/* Children */
	.children-empty { font-size: 10px; color: var(--hud-muted); padding: 6px 14px 12px 34px; font-style: italic; }
	.child-list { list-style: none; margin: 0; padding: 0 14px 10px 34px; display: flex; flex-direction: column; gap: 5px; border-top: 1px solid var(--hud-dim); padding-top: 8px; }
	.child-item { display: flex; align-items: center; gap: 8px; padding: 5px 8px; background: #080b10; border-left: 2px solid var(--hud-dim); font-size: 11px; }
	.child-id { font-size: 9px; color: var(--hud-cyan); opacity: 0.6; font-family: var(--font-hud, monospace); flex-shrink: 0; min-width: 80px; }
	.child-title { flex: 1; color: var(--hud-text); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.child-type { font-size: 8px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 1px 4px; border: 1px solid; flex-shrink: 0; }
	.type-task    { color: #94a3b8; border-color: #94a3b844; }
	.type-epic    { color: #a78bfa; border-color: #a78bfa66; }
	.type-story   { color: #34d399; border-color: #34d39966; }
	.type-bug     { color: #f87171; border-color: #f8717166; }
	.type-subtask { color: #60a5fa; border-color: #60a5fa66; }
	.child-priority { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.1em; padding: 1px 5px; border: 1px solid; flex-shrink: 0; }
	.priority-critical { color: #ef4444; border-color: #ef444466; }
	.priority-high { color: #f97316; border-color: #f9731666; }
	.priority-medium { color: var(--hud-cyan); border-color: #00d4ff44; }
	.priority-low { color: #6b7280; border-color: #37415166; }
	.child-status { font-size: 9px; font-family: var(--font-hud, monospace); letter-spacing: 0.08em; padding: 1px 5px; border: 1px solid; flex-shrink: 0; }
	.status-unassigned  { color: #6b7280; border-color: #37415166; }
	.status-assigned    { color: #60a5fa; border-color: #60a5fa44; }
	.status-inprogress  { color: #f59e0b; border-color: #f59e0b44; }
	.status-testing     { color: #a78bfa; border-color: #a78bfa44; }
	.status-review      { color: #f97316; border-color: #f9731644; }
	.status-done        { color: #22c55e; border-color: #22c55e44; }
	.child-assignee { font-size: 9px; color: var(--hud-muted); font-family: var(--font-hud, monospace); flex-shrink: 0; }

	.empty-msg { font-size: 12px; letter-spacing: 0.15em; color: var(--hud-muted); padding: 40px 0; text-align: center; }
</style>
