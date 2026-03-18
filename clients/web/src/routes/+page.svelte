<script lang="ts">
	import { onMount } from 'svelte';
	import { subjectId } from '$lib/stores/auth';

	const services = [
		{ name: 'business-ops', label: 'Business Ops',  icon: '◧' },
		{ name: 'facility',     label: 'Facility',      icon: '◈' },
		{ name: 'intelligence', label: 'Intelligence',  icon: '◎' },
		{ name: 'learning',     label: 'Learning',      icon: '◉' },
		{ name: 'nlp',          label: 'NLP / Dialogue',icon: '◑' },
		{ name: 'security',     label: 'Security',      icon: '◬' },
		{ name: 'voice',        label: 'Voice',         icon: '◐' },
	];

	let health = $state<'checking' | 'online' | 'offline'>('checking');
	let grpcPort = $state('50051');
	let restPort = $state('8080');
	let now = $state(new Date().toISOString().slice(0, 19).replace('T', ' '));

	onMount(async () => {
		const interval = setInterval(() => {
			now = new Date().toISOString().slice(0, 19).replace('T', ' ');
		}, 1000);

		try {
			const res = await fetch('/v1/security/audit?page_size=1');
			health = res.status < 500 ? 'online' : 'offline';
		} catch {
			health = 'offline';
		}

		return () => clearInterval(interval);
	});
</script>

<div class="dashboard">
	<div class="dash-header">
		<span class="hud-label" style="font-size:13px">SYSTEM STATUS</span>
		<span class="text-muted" style="font-size:11px">{now} UTC</span>
	</div>

	<div class="dash-grid">
		<!-- Health panel -->
		<div class="hud-panel panel-health">
			<div class="panel-title hud-label">CORE SYSTEM</div>
			<div class="health-status">
				<span class="status-dot {health === 'online' ? 'online' : health === 'offline' ? 'offline' : ''}"></span>
				<span class="health-text font-hud" class:text-green={health === 'online'} class:text-red={health === 'offline'}>
					{health === 'checking' ? 'INITIALIZING...' : health === 'online' ? 'ONLINE' : 'OFFLINE'}
				</span>
			</div>
			<div class="endpoint-row">
				<span class="text-muted">REST</span>
				<span class="text-cyan">:8080</span>
			</div>
			<div class="endpoint-row">
				<span class="text-muted">gRPC</span>
				<span class="text-cyan">:50051</span>
			</div>
			<div class="endpoint-row">
				<span class="text-muted">OPERATOR</span>
				<span class="text-amber">{$subjectId ?? '—'}</span>
			</div>
		</div>

		<!-- Services panel -->
		<div class="hud-panel panel-services">
			<div class="panel-title hud-label">ACTIVE SERVICES</div>
			<div class="services-list">
				{#each services as svc}
					<div class="service-row">
						<span class="svc-icon text-cyan">{svc.icon}</span>
						<span class="svc-name">{svc.label}</span>
						<span class="status-dot online"></span>
					</div>
				{/each}
			</div>
		</div>

		<!-- Quick actions panel -->
		<div class="hud-panel hud-panel-amber panel-actions">
			<div class="panel-title hud-label hud-label-amber">QUICK ACCESS</div>
			<div class="actions-list">
				<a href="/dialogue" class="hud-btn hud-btn-amber action-btn">◉ START DIALOGUE</a>
				<a href="/schedule" class="hud-btn action-btn">◷ SCHEDULE EVENT</a>
				<a href="/intel"    class="hud-btn action-btn">◎ QUERY INTEL</a>
				<a href="/security" class="hud-btn action-btn">◬ SECURITY PANEL</a>
			</div>
		</div>
	</div>
</div>

<style>
	.dashboard { display: flex; flex-direction: column; gap: 16px; height: 100%; }
	.dash-header { display: flex; justify-content: space-between; align-items: center; }
	.dash-grid { display: grid; grid-template-columns: 220px 1fr 200px; gap: 16px; flex: 1; }

	.hud-panel { padding: 16px; }
	.panel-title { margin-bottom: 16px; display: block; }

	.panel-health { display: flex; flex-direction: column; gap: 10px; }
	.health-status { display: flex; align-items: center; gap: 10px; margin-bottom: 4px; }
	.health-text { font-size: 18px; letter-spacing: 0.1em; }
	.endpoint-row { display: flex; justify-content: space-between; font-size: 12px; padding: 4px 0; border-bottom: 1px solid var(--hud-dim); }

	.services-list { display: flex; flex-direction: column; gap: 6px; }
	.service-row { display: flex; align-items: center; gap: 10px; padding: 6px 0; border-bottom: 1px solid var(--hud-dim); font-size: 12px; }
	.svc-icon { width: 16px; text-align: center; }
	.svc-name { flex: 1; }

	.actions-list { display: flex; flex-direction: column; gap: 8px; }
	.action-btn { display: block; text-align: center; text-decoration: none; font-size: 11px; }
</style>
