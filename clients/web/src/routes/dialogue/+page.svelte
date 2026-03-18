<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { subjectId } from '$lib/stores/auth';
	import { nlp } from '$lib/api/client';

	interface Message {
		role: 'user' | 'jarvis';
		text: string;
		intent?: string;
		ts: string;
	}

	let sessionId = $state(`web-${Date.now()}`);
	let messages = $state<Message[]>([]);
	let input = $state('');
	let loading = $state(false);
	let error = $state('');
	let scrollEl: HTMLDivElement;

	function timestamp() {
		return new Date().toISOString().slice(11, 19);
	}

	async function send(e: Event) {
		e.preventDefault();
		const text = input.trim();
		if (!text || loading) return;

		input = '';
		error = '';
		messages = [...messages, { role: 'user', text, ts: timestamp() }];
		loading = true;

		await tick();
		scrollEl?.scrollTo({ top: scrollEl.scrollHeight, behavior: 'smooth' });

		try {
			const res = await nlp.dialogue(sessionId, text, $subjectId ?? 'web-user');
			messages = [...messages, {
				role: 'jarvis',
				text: res.replyText,
				intent: res.resolvedIntent,
				ts: timestamp()
			}];
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Communication failure';
		} finally {
			loading = false;
			await tick();
			scrollEl?.scrollTo({ top: scrollEl.scrollHeight, behavior: 'smooth' });
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey) {
			send(e);
		}
	}

	function newSession() {
		sessionId = `web-${Date.now()}`;
		messages = [];
		error = '';
	}

	onMount(() => {
		messages = [{
			role: 'jarvis',
			text: `Good day. I am J.A.R.V.I.S. Session ${sessionId} initialized. How may I assist you?`,
			ts: timestamp()
		}];
	});
</script>

<div class="dialogue-layout">
	<!-- Session header -->
	<div class="dialogue-header">
		<span class="hud-label">DIALOGUE SESSION</span>
		<span class="text-muted session-id">{sessionId}</span>
		<button class="hud-btn" style="font-size:10px;padding:4px 10px" onclick={newSession}>NEW SESSION</button>
	</div>

	<!-- Message feed -->
	<div class="msg-feed hud-panel" bind:this={scrollEl}>
		{#each messages as msg}
			<div class="msg-row msg-{msg.role} fade-in">
				<div class="msg-meta">
					<span class="msg-role hud-label" class:hud-label-amber={msg.role === 'user'}>
						{msg.role === 'user' ? ($subjectId ?? 'USER') : 'J.A.R.V.I.S.'}
					</span>
					{#if msg.intent}
						<span class="msg-intent text-muted">[{msg.intent.replace('INTENT_', '')}]</span>
					{/if}
					<span class="msg-ts text-muted">{msg.ts}</span>
				</div>
				<div class="msg-text">{msg.text}</div>
			</div>
		{/each}

		{#if loading}
			<div class="msg-row msg-jarvis fade-in">
				<div class="msg-meta">
					<span class="hud-label">J.A.R.V.I.S.</span>
				</div>
				<div class="msg-text text-muted">
					Processing<span class="cursor">_</span>
				</div>
			</div>
		{/if}

		{#if error}
			<div class="msg-error text-red">{error}</div>
		{/if}
	</div>

	<!-- Input bar -->
	<form class="input-bar" onsubmit={send}>
		<span class="input-prompt text-cyan font-hud">›</span>
		<input
			class="hud-input dialogue-input"
			bind:value={input}
			onkeydown={handleKeydown}
			placeholder="Speak your command, sir..."
			disabled={loading}
			autocomplete="off"
		/>
		<button type="submit" class="hud-btn send-btn" disabled={loading || !input.trim()}>
			TRANSMIT
		</button>
	</form>
</div>

<style>
	.dialogue-layout { display: grid; grid-template-rows: auto 1fr auto; height: 100%; gap: 12px; }

	.dialogue-header { display: flex; align-items: center; gap: 12px; }
	.session-id { font-size: 11px; flex: 1; }

	.msg-feed { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; min-height: 0; }

	.msg-row { display: flex; flex-direction: column; gap: 4px; padding: 10px 12px; border-left: 2px solid transparent; }
	.msg-jarvis { border-left-color: var(--hud-cyan); background: #00d4ff06; }
	.msg-user   { border-left-color: var(--hud-amber); background: #ff8c0006; align-items: flex-end; text-align: right; }

	.msg-meta { display: flex; align-items: center; gap: 8px; font-size: 10px; }
	.msg-user .msg-meta { flex-direction: row-reverse; }
	.msg-ts { font-size: 10px; }
	.msg-intent { font-size: 10px; }

	.msg-text { font-size: 13px; line-height: 1.6; white-space: pre-wrap; }

	.msg-error { font-size: 12px; padding: 8px; border: 1px solid var(--hud-red); background: #ff2d5510; }

	.input-bar { display: flex; align-items: center; gap: 8px; }
	.input-prompt { font-size: 18px; line-height: 1; }
	.dialogue-input { border-color: var(--hud-cyan); }
	.send-btn { white-space: nowrap; flex-shrink: 0; }
</style>
