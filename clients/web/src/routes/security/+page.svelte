<script lang="ts">
	import { security, type AnalyzeFacesResponse, type AuditLogResponse, type AnalyzeThreatSceneResponse, type LogThreatEventResponse, type ThreatLevel } from '$lib/api/client';
	import * as cocoSsd from '@tensorflow-models/coco-ssd';
	import '@tensorflow/tfjs';

	type Mode = 'threat' | 'audit' | 'faces';
	let mode = $state<Mode>('faces');

	// ── Threat / camera ───────────────────────────────────────────────
	let cameras       = $state<MediaDeviceInfo[]>([]);
	let selectedCamId = $state('');
	let camError      = $state('');
	let camActive     = $state(false);

	// DOM refs — $state required in Svelte 5 for bind:this
	let videoEl  = $state<HTMLVideoElement | undefined>(undefined);
	let canvasEl = $state<HTMLCanvasElement | undefined>(undefined);
	let rafId    = 0;
	let camStream: MediaStream | null = null;

	// ── Threat tier classification ────────────────────────────────────
	type Tier = { color: string; badge: string; scanSpeed: number };

	const TIERS: Record<string, Tier> = {
		// CRITICAL — weapons
		knife:        { color: '#ff2d55', badge: 'CRITICAL', scanSpeed: 3 },
		scissors:     { color: '#ff2d55', badge: 'CRITICAL', scanSpeed: 3 },
		// HIGH — persons and fast-moving vehicles
		person:       { color: '#ff6600', badge: 'HIGH',     scanSpeed: 2 },
		motorcycle:   { color: '#ff6600', badge: 'HIGH',     scanSpeed: 2 },
		// TRACK — monitored vehicles / animals
		car:          { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
		truck:        { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
		bus:          { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
		bicycle:      { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
		dog:          { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
		cat:          { color: '#ffaa00', badge: 'TRACK',    scanSpeed: 1.5 },
	};
	const BENIGN_TIER: Tier = { color: '#00d4ff', badge: 'OBJECT', scanSpeed: 1 };

	function tier(cls: string): Tier {
		return TIERS[cls.toLowerCase()] ?? BENIGN_TIER;
	}

	// ── COCO-SSD object detection ─────────────────────────────────────
	let cocoModel     = $state<cocoSsd.ObjectDetection | null>(null);
	let modelLoading  = $state(false);
	let detections    = $state<cocoSsd.DetectedObject[]>([]);
	let frameCount    = 0; // run detection every 6th frame (~10 fps at 60 Hz)

	async function loadModel() {
		if (cocoModel || modelLoading) return;
		modelLoading = true;
		try {
			cocoModel = await cocoSsd.load();
		} catch {
			// model load failure is non-fatal — canvas still works
		} finally {
			modelLoading = false;
		}
	}

	// ── Claude Vision threat analysis ─────────────────────────────────
	let visionResult   = $state<AnalyzeThreatSceneResponse | null>(null);
	let visionLoading  = $state(false);
	let visionError    = $state('');
	let logMode        = $state('manual'); // updated from first AnalyzeThreatScene response
	let lastFrameB64   = '';              // retained for logging after analysis

	// Returns true if LOG button should be visible
	const showLogButton = $derived(
		camActive && visionResult != null && (logMode === 'manual' || logMode === 'all')
	);

	async function analyzeScene() {
		if (!canvasEl || !camActive) return;
		visionError   = '';
		visionResult  = null;
		visionLoading = true;
		try {
			const dataUrl  = canvasEl.toDataURL('image/jpeg', 0.85);
			lastFrameB64   = dataUrl.slice(dataUrl.indexOf(',') + 1);
			const labels   = detections.map(d => `${d.class}(${d.score.toFixed(2)})`);
			visionResult   = await security.analyzeThreatScene(lastFrameB64, labels);
			logMode        = visionResult.logMode || 'manual';

			// Auto-log when mode is "auto" or "all"
			if (logMode === 'auto' || logMode === 'all') {
				sendLogEvent(false);
			}
		} catch (e) {
			visionError = e instanceof Error ? e.message : String(e);
		} finally {
			visionLoading = false;
		}
	}

	let logLoading = $state(false);
	let logResult  = $state<LogThreatEventResponse | null>(null);

	async function sendLogEvent(force: boolean) {
		if (!visionResult) return;
		logLoading = true;
		try {
			const camLabel = cameras.find(c => c.deviceId === selectedCamId)?.label ?? 'CAMERA-01';
			const labels   = detections.map(d => `${d.class}(${d.score.toFixed(2)})`);
			logResult = await security.logThreatEvent({
				cameraLabel:        camLabel,
				detectedObjects:    labels,
				level:              visionResult.level,
				confidence:         visionResult.confidence,
				threatSummary:      visionResult.threatSummary,
				recommendedActions: visionResult.recommendedActions,
				imageData:          lastFrameB64,
				force
			});
		} catch { /* log failures are non-fatal */ } finally {
			logLoading = false;
		}
	}

	function threatRowColor(lvl: string): string {
		if (lvl.includes('CRITICAL')) return '#ff2d55';
		if (lvl.includes('HIGH'))     return '#ff6600';
		if (lvl.includes('MODERATE')) return '#ffaa00';
		if (lvl.includes('LOW'))      return '#00d4ff';
		return '#666';
	}

	const threatColors: Record<ThreatLevel, string> = {
		THREAT_LEVEL_UNSPECIFIED: '#666',
		THREAT_LEVEL_LOW:         '#00d4ff',
		THREAT_LEVEL_MODERATE:    '#ffaa00',
		THREAT_LEVEL_HIGH:        '#ff6600',
		THREAT_LEVEL_CRITICAL:    '#ff2d55',
	};
	const threatLabels: Record<ThreatLevel, string> = {
		THREAT_LEVEL_UNSPECIFIED: 'UNKNOWN',
		THREAT_LEVEL_LOW:         'LOW',
		THREAT_LEVEL_MODERATE:    'MODERATE',
		THREAT_LEVEL_HIGH:        'HIGH',
		THREAT_LEVEL_CRITICAL:    'CRITICAL',
	};

	async function enumerateCameras() {
		try {
			const devices = await navigator.mediaDevices.enumerateDevices();
			cameras = devices.filter(d => d.kind === 'videoinput');
			if (cameras.length > 0 && !selectedCamId) selectedCamId = cameras[0].deviceId;
		} catch {
			camError = 'Could not list cameras.';
		}
	}

	async function startCamera() {
		camError = '';
		if (camStream) stopCamera();
		try {
			camStream = await navigator.mediaDevices.getUserMedia({
				video: selectedCamId ? { deviceId: { exact: selectedCamId } } : true,
				audio: false
			});
			if (!videoEl) return;
			videoEl.srcObject = camStream;
			await videoEl.play();
			camActive = true;
			await enumerateCameras(); // re-enumerate after grant to populate labels
			loadModel();              // kick off COCO-SSD load in parallel
			rafId = requestAnimationFrame(renderLoop);
		} catch (e) {
			camError = e instanceof Error ? e.message : 'Camera access denied.';
			camActive = false;
		}
	}

	function stopCamera() {
		cancelAnimationFrame(rafId);
		camStream?.getTracks().forEach(t => t.stop());
		camStream    = null;
		camActive    = false;
		detections   = [];
		visionResult = null;
		canvasEl?.getContext('2d')?.clearRect(0, 0, canvasEl.width, canvasEl.height);
	}

	async function renderLoop() {
		if (!videoEl || !canvasEl || !camStream) return;
		const ctx = canvasEl.getContext('2d');
		if (!ctx) return;
		if (videoEl.videoWidth > 0) {
			canvasEl.width  = videoEl.videoWidth;
			canvasEl.height = videoEl.videoHeight;
		}
		ctx.drawImage(videoEl as CanvasImageSource, 0, 0);

		// Run COCO-SSD every 6th frame to keep CPU reasonable
		if (cocoModel && frameCount % 6 === 0) {
			try {
				detections = await cocoModel.detect(videoEl as HTMLVideoElement);
			} catch { /* ignore transient errors */ }
		}
		frameCount++;

		drawDetections(ctx, detections);
		drawHUD(ctx, canvasEl.width, canvasEl.height);
		rafId = requestAnimationFrame(renderLoop);
	}

	function drawDetections(ctx: CanvasRenderingContext2D, dets: cocoSsd.DetectedObject[]) {
		if (!dets.length) return;
		const t = Date.now() / 1000; // seconds, for animations

		// Identify highest-threat target for lock-on reticule
		const priorities = ['CRITICAL', 'HIGH', 'TRACK', 'OBJECT'];
		const lockOn = dets.reduce((best, d) => {
			const bi = priorities.indexOf(tier(best.class).badge);
			const di = priorities.indexOf(tier(d.class).badge);
			return di < bi || (di === bi && d.score > best.score) ? d : best;
		}, dets[0]);

		for (const d of dets) {
			const [x, y, w, h] = d.bbox;
			const tk            = tier(d.class);
			const col           = tk.color;
			const isLockOn      = d === lockOn;
			const bk            = 16; // bracket arm length

			// ── Scan line ────────────────────────────────────────────
			const scanY = y + (h * ((t * tk.scanSpeed) % 1));
			const grad  = ctx.createLinearGradient(x, scanY - 4, x, scanY + 4);
			grad.addColorStop(0,   col + '00');
			grad.addColorStop(0.5, col + 'aa');
			grad.addColorStop(1,   col + '00');
			ctx.fillStyle = grad;
			ctx.fillRect(x, scanY - 4, w, 8);

			// ── Corner-bracket box ───────────────────────────────────
			ctx.lineWidth   = isLockOn ? 2 : 1.5;
			ctx.strokeStyle = col;
			ctx.shadowColor = col;
			ctx.shadowBlur  = isLockOn ? 14 : 8;
			// Top-left
			ctx.beginPath(); ctx.moveTo(x, y + bk); ctx.lineTo(x, y); ctx.lineTo(x + bk, y); ctx.stroke();
			// Top-right
			ctx.beginPath(); ctx.moveTo(x+w-bk, y); ctx.lineTo(x+w, y); ctx.lineTo(x+w, y+bk); ctx.stroke();
			// Bottom-left
			ctx.beginPath(); ctx.moveTo(x, y+h-bk); ctx.lineTo(x, y+h); ctx.lineTo(x+bk, y+h); ctx.stroke();
			// Bottom-right
			ctx.beginPath(); ctx.moveTo(x+w-bk, y+h); ctx.lineTo(x+w, y+h); ctx.lineTo(x+w, y+h-bk); ctx.stroke();
			ctx.shadowBlur = 0;

			// ── Pulse ring (CRITICAL only) ───────────────────────────
			if (tk.badge === 'CRITICAL') {
				const pulse  = (t * 2) % 1;           // 0→1 every 0.5 s
				const radius = (Math.max(w, h) / 2) * (0.55 + pulse * 0.45);
				ctx.beginPath();
				ctx.arc(x + w / 2, y + h / 2, radius, 0, Math.PI * 2);
				ctx.strokeStyle = col + Math.round((1 - pulse) * 0xcc).toString(16).padStart(2, '0');
				ctx.lineWidth   = 1;
				ctx.shadowColor = col;
				ctx.shadowBlur  = 8;
				ctx.stroke();
				ctx.shadowBlur = 0;
			}

			// ── Label panel ──────────────────────────────────────────
			const panelH  = 28;
			const panelY  = y > panelH + 2 ? y - panelH - 2 : y + h + 2;
			const conf    = d.score;
			const badgeW  = 52;
			const classLabel = d.class.toUpperCase();
			ctx.font      = 'bold 9px monospace';
			const classW  = ctx.measureText(classLabel).width + 8;
			const panelW  = badgeW + classW + 4;

			// Panel background
			ctx.fillStyle = '#000e';
			ctx.fillRect(x, panelY, panelW, panelH);
			ctx.strokeStyle = col + '66';
			ctx.lineWidth   = 1;
			ctx.strokeRect(x, panelY, panelW, panelH);

			// Tier badge
			ctx.fillStyle = col + '33';
			ctx.fillRect(x, panelY, badgeW, panelH);
			ctx.fillStyle = col;
			ctx.font      = 'bold 8px monospace';
			ctx.fillText(tk.badge, x + 4, panelY + 11);

			// Confidence bar within badge
			const barY = panelY + 17;
			ctx.fillStyle = '#ffffff22';
			ctx.fillRect(x + 4, barY, badgeW - 8, 4);
			ctx.fillStyle = col;
			ctx.fillRect(x + 4, barY, (badgeW - 8) * conf, 4);

			// Class name
			ctx.fillStyle = '#fff';
			ctx.font      = 'bold 9px monospace';
			ctx.fillText(classLabel, x + badgeW + 4, panelY + 12);
			// Confidence %
			ctx.fillStyle = col + 'bb';
			ctx.font      = '8px monospace';
			ctx.fillText(`${Math.round(conf * 100)}%`, x + badgeW + 4, panelY + 22);

			// ── Lock-on reticule ─────────────────────────────────────
			if (isLockOn && dets.length > 1) {
				const cx      = x + w / 2;
				const cy      = y + h / 2;
				const arm     = 10;
				const gap     = 5;
				const angle   = (t * 1.5) % (Math.PI * 2); // slow rotation
				ctx.save();
				ctx.translate(cx, cy);
				ctx.rotate(angle);
				ctx.strokeStyle = col;
				ctx.lineWidth   = 1.5;
				ctx.shadowColor = col;
				ctx.shadowBlur  = 10;
				// 4 rotated tick marks
				for (let i = 0; i < 4; i++) {
					ctx.rotate(Math.PI / 2);
					ctx.beginPath();
					ctx.moveTo(0, gap);
					ctx.lineTo(0, gap + arm);
					ctx.stroke();
				}
				// Center dot
				ctx.shadowBlur = 0;
				ctx.beginPath();
				ctx.arc(0, 0, 2.5, 0, Math.PI * 2);
				ctx.fillStyle = col;
				ctx.fill();
				ctx.restore();
			}
		}
	}

	function drawHUD(ctx: CanvasRenderingContext2D, w: number, h: number) {
		if (!w || !h) return;
		const red  = '#ff2d55';
		const cyan = '#00d4ff';
		const sz   = 28;

		// Glow corner brackets
		ctx.strokeStyle = red;
		ctx.lineWidth   = 2;
		ctx.shadowColor = red;
		ctx.shadowBlur  = 10;
		ctx.beginPath(); ctx.moveTo(0,    sz); ctx.lineTo(0,  0);  ctx.lineTo(sz,   0);  ctx.stroke(); // TL
		ctx.beginPath(); ctx.moveTo(w-sz, 0);  ctx.lineTo(w,  0);  ctx.lineTo(w,    sz); ctx.stroke(); // TR
		ctx.beginPath(); ctx.moveTo(0,  h-sz); ctx.lineTo(0,  h);  ctx.lineTo(sz,   h);  ctx.stroke(); // BL
		ctx.beginPath(); ctx.moveTo(w-sz, h);  ctx.lineTo(w,  h);  ctx.lineTo(w, h-sz);  ctx.stroke(); // BR
		ctx.shadowBlur = 0;

		// Top-left: model status
		ctx.font      = 'bold 10px monospace';
		ctx.fillStyle = red + 'cc';
		const modelTag = modelLoading ? 'MODEL LOADING...' : cocoModel ? 'THREAT SCAN ACTIVE' : 'THREAT SCAN ACTIVE';
		ctx.fillText(modelTag, 8, 18);

		// Top-right: LIVE badge + object count
		ctx.font      = 'bold 11px monospace';
		ctx.fillStyle = red;
		const liveW = ctx.measureText('● LIVE').width;
		ctx.fillText('● LIVE', w - liveW - 8, 18);
		if (detections.length > 0) {
			// Show highest active tier in the count badge
			const priorities = ['CRITICAL', 'HIGH', 'TRACK', 'OBJECT'];
			const topTier = detections.reduce((best, d) => {
				const bi = priorities.indexOf(tier(best.class).badge);
				const di = priorities.indexOf(tier(d.class).badge);
				return di < bi ? d : best;
			}, detections[0]);
			const tk     = tier(topTier.class);
			ctx.font      = 'bold 9px monospace';
			ctx.fillStyle = tk.color;
			ctx.shadowColor = tk.color;
			ctx.shadowBlur  = 6;
			const objTag  = `${detections.length} OBJ · ${tk.badge}`;
			const objW    = ctx.measureText(objTag).width;
			ctx.fillText(objTag, w - objW - 8, 32);
			ctx.shadowBlur = 0;
		}

		// Bottom-left: timestamp
		ctx.font      = '9px monospace';
		ctx.fillStyle = cyan;
		ctx.fillText(new Date().toLocaleTimeString(), 8, h - 8);

		// Bottom-right: camera label
		const camLabel = cameras.find(c => c.deviceId === selectedCamId)?.label ?? 'CAMERA-01';
		const short    = camLabel.substring(0, 28).toUpperCase();
		ctx.fillStyle  = cyan + '88';
		const labelW   = ctx.measureText(short).width;
		ctx.fillText(short, w - labelW - 8, h - 8);
	}

	// Stop camera when leaving threat mode
	$effect(() => { if (mode !== 'threat') stopCamera(); });
	// Enumerate cameras when entering threat mode
	$effect(() => { if (mode === 'threat') enumerateCameras(); });
	// Cleanup on component destroy
	$effect(() => () => stopCamera());

	// ── Faces ─────────────────────────────────────────────────────────
	let selectedFile  = $state<File | null>(null);
	let previewUrl    = $state('');
	let facesResult   = $state<AnalyzeFacesResponse | null>(null);
	let showFacesModal = $state(false);

	$effect(() => () => { if (previewUrl) URL.revokeObjectURL(previewUrl); });

	function handleFileChange(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file  = input.files?.[0] ?? null;
		selectedFile = file;
		if (previewUrl) URL.revokeObjectURL(previewUrl);
		previewUrl = file ? URL.createObjectURL(file) : '';
		facesResult = null;
		error = '';
	}

	// ── Audit ─────────────────────────────────────────────────────────
	let auditResult = $state<AuditLogResponse | null>(null);

	let loading = $state(false);
	let result  = $state<unknown>(null);
	let error   = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = ''; result = null; loading = true;
		try {
			if (mode === 'audit') {
				await fetchAudit();
				return;
			} else if (mode === 'faces') {
				if (!selectedFile) { error = 'Select an image first.'; loading = false; return; }
				const b64 = await readFileAsBase64(selectedFile);
				facesResult = await security.analyzeFaces(b64, selectedFile.name);
				showFacesModal = true;
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Security request failed';
		} finally {
			loading = false;
		}
	}

	async function fetchAudit() {
		error = ''; loading = true;
		try {
			auditResult = await security.auditLog(20);
			result = auditResult;
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Audit retrieval failed';
		} finally {
			loading = false;
		}
	}

	function closeFacesModal() { showFacesModal = false; }

	function readFileAsBase64(file: File): Promise<string> {
		return new Promise((resolve, reject) => {
			const reader = new FileReader();
			reader.onload = () => {
				const dataUrl = reader.result as string;
				resolve(dataUrl.slice(dataUrl.indexOf(',') + 1));
			};
			reader.onerror = () => reject(reader.error);
			reader.readAsDataURL(file);
		});
	}
</script>

<!-- Hidden video element — always present for camera capture -->
<!-- svelte-ignore a11y_media_has_caption -->
<video bind:this={videoEl} muted playsinline aria-hidden="true" style="position:absolute;width:1px;height:1px;opacity:0;pointer-events:none;top:-9999px"></video>

<div class="page-layout">
	<div class="hud-label" style="font-size:13px;margin-bottom:16px;color:var(--hud-red);text-shadow:var(--glow-red)">
		SECURITY PANEL
	</div>

	<div class="grid-2" class:cam-grid={mode === 'threat'}>
		<!-- ── Left panel ─────────────────────────────────────────── -->
		<div class="hud-panel form-panel" style="border-color:var(--hud-red);box-shadow:var(--glow-red),inset 0 0 20px #ff2d5508">
			<div class="mode-tabs">
				{#each (['threat', 'audit', 'faces'] as Mode[]) as m}
					<button class="hud-btn mode-tab" class:active={mode === m}
						onclick={() => { mode = m; result = null; auditResult = null; error = ''; if (m === 'audit') fetchAudit(); }}>
						{m.toUpperCase()}
					</button>
				{/each}
			</div>

			<p class="tab-description">
				{#if mode === 'threat'}
					REAL-TIME CAMERA FEED WITH AR OVERLAY
				{:else if mode === 'audit'}
					COMPILES AN OVERALL SURROUNDINGS SENTIMENT
				{:else}
					LOAD AN IMAGE FOR FACIAL SENTIMENT ANALYSIS
				{/if}
			</p>

			{#if mode === 'threat'}
				<!-- Camera controls — not a form submit -->
				<div class="cam-controls">
					<div class="field">
						<label class="hud-label" for="cam-select">CAMERA SOURCE</label>
						<select id="cam-select" class="hud-input" bind:value={selectedCamId} disabled={camActive}>
							{#if cameras.length === 0}
								<option value="">— no cameras detected —</option>
							{:else}
								{#each cameras as cam, i}
									<option value={cam.deviceId}>{cam.label || `Camera ${i + 1}`}</option>
								{/each}
							{/if}
						</select>
					</div>

					<div class="cam-status" class:cam-status-active={camActive}>
						<span class="cam-dot"></span>
						{camActive ? 'FEED ACTIVE' : 'FEED INACTIVE'}
					</div>

					{#if camError}
						<div class="cam-error">{camError}</div>
					{/if}
				</div>

				<div class="cam-actions">
					{#if !camActive}
						<button class="hud-btn submit-btn hud-btn-red" onclick={startCamera}>
							ACTIVATE FEED
						</button>
					{:else}
						<button class="hud-btn submit-btn hud-btn-cyan" onclick={analyzeScene}
							disabled={visionLoading}>
							{visionLoading ? 'ANALYZING...' : 'ANALYZE SCENE'}
						</button>
						{#if showLogButton}
							<button class="hud-btn submit-btn hud-btn-log" style="margin-top:6px"
								onclick={() => sendLogEvent(true)} disabled={logLoading}>
								{logLoading ? 'LOGGING...' : logResult?.logged ? '✓ LOGGED' : 'LOG THREAT'}
							</button>
						{/if}
						<button class="hud-btn submit-btn cam-stop-btn" style="margin-top:6px" onclick={stopCamera}>
							DEACTIVATE
						</button>
					{/if}
				</div>
			{:else}
				<form onsubmit={handleSubmit} class="form">
					{#if mode === 'audit'}
						<p class="text-muted" style="font-size:12px">Retrieve the last 20 audit log entries.</p>
					{:else}
						<div class="field">
							<label class="hud-label" for="img-upload">SELECT IMAGE</label>
							<label class="hud-file-label" for="img-upload">
								{selectedFile ? selectedFile.name : 'CHOOSE FILE...'}
							</label>
							<input id="img-upload" type="file" accept="image/*" class="hud-file-input" onchange={handleFileChange} />
						</div>
						{#if previewUrl}
							<div class="preview-wrap">
								<img src={previewUrl} alt="Preview" class="preview-img" />
							</div>
						{/if}
					{/if}

					<button type="submit" class="hud-btn submit-btn hud-btn-red" disabled={loading}>
						{#if loading}PROCESSING...
						{:else if mode === 'audit'}RETRIEVE AUDIT LOG
						{:else}ANALYZE FACES
						{/if}
					</button>
				</form>
			{/if}
		</div>

		<!-- ── Right panel ────────────────────────────────────────── -->
		<div class="hud-panel result-panel" style="border-color:var(--hud-red)" class:cam-result-panel={mode === 'threat'}>
			{#if mode === 'threat'}
				<div class="cam-view">
					<canvas bind:this={canvasEl} class="cam-canvas" class:cam-hidden={!camActive}></canvas>
					{#if !camActive}
						<div class="cam-placeholder">
							<div class="hud-label cam-placeholder-title">NO FEED</div>
							{#if camError}
								<div class="cam-error" style="margin-top:10px">{camError}</div>
							{:else}
								<div class="text-muted" style="font-size:11px;margin-top:8px;letter-spacing:0.05em">
									SELECT A CAMERA SOURCE AND ACTIVATE FEED
								</div>
							{/if}
						</div>
					{/if}

					<!-- Detection sidebar — shown below canvas when active -->
					{#if camActive && (detections.length > 0 || visionResult || visionError || visionLoading)}
						<div class="detect-sidebar">
							{#if detections.length > 0}
								<div class="detect-section">
									<div class="hud-label detect-title">DETECTED OBJECTS</div>
									{#each detections as d}
										<div class="detect-row">
											<span class="detect-class">{d.class.toUpperCase()}</span>
											<span class="detect-conf">{Math.round(d.score * 100)}%</span>
										</div>
									{/each}
								</div>
							{/if}

							{#if visionLoading}
								<div class="detect-section">
									<div class="scan-bar" style="margin-top:4px"></div>
									<div class="detect-title hud-label" style="margin-top:6px;font-size:9px">CLAUDE ANALYZING...</div>
								</div>
							{:else if visionError}
								<div class="detect-section">
									<div class="cam-error">{visionError}</div>
								</div>
							{:else if visionResult}
								{@const lvl = visionResult.level ?? 'THREAT_LEVEL_UNSPECIFIED'}
								<div class="detect-section vision-result">
									<div class="hud-label detect-title">THREAT ASSESSMENT</div>
									<div class="vision-level" style="color:{threatColors[lvl]}">
										{threatLabels[lvl]}
										<span class="vision-conf">({Math.round(visionResult.confidence * 100)}%)</span>
									</div>
									<p class="vision-summary">{visionResult.threatSummary}</p>
									{#if visionResult.recommendedActions?.length}
										<div class="vision-actions">
											{#each visionResult.recommendedActions as action}
												<div class="vision-action">▶ {action}</div>
											{/each}
										</div>
									{/if}
								</div>
							{/if}
						</div>
					{/if}
				</div>
			{:else}
				<div class="panel-title hud-label" style="color:var(--hud-red)">ASSESSMENT RESULTS</div>
				{#if loading && mode === 'faces'}
					<div class="scan-wrap">
						<div class="scan-bar"></div>
						<div class="scan-label hud-label">SCANNING BIOMETRIC DATA...</div>
					</div>
				{:else if error}
					<div class="text-red" style="font-size:12px">{error}</div>
				{:else if mode === 'audit' && auditResult}
					{#if auditResult.surroundingsStatus}
						{@const ss = auditResult.surroundingsStatus}
						<div class="status-badge status-{ss.color.toLowerCase()}" style="margin-bottom:16px">
							<div class="status-label">{ss.status}</div>
							<div class="status-score">{ss.score.toFixed(1)}</div>
							<div class="status-color-label">{ss.color}</div>
						</div>
					{/if}
					{#if auditResult.entries?.length}
						<div class="audit-entries">
							{#each auditResult.entries as entry}
								{@const isThreat = entry.action.startsWith('threat-event:')}
								{@const threatLvl = isThreat ? entry.action.split(':')[1] : ''}
								<div class="audit-row" class:audit-row-threat={isThreat}
									style={isThreat ? `--row-color:${threatRowColor(threatLvl)}` : ''}>
									<span class="audit-action" class:audit-action-threat={isThreat}>
										{#if isThreat}
											<span class="threat-badge" style="color:var(--row-color);border-color:var(--row-color)">
												{threatLvl.replace('THREAT_LEVEL_', '')}
											</span>
										{/if}
										{entry.action}
									</span>
									<span class="audit-subject">{entry.subjectId}</span>
									<span class="audit-ts">{new Date(entry.timestamp).toLocaleTimeString()}</span>
									<span class="audit-ok" class:audit-fail={!entry.success}>{entry.success ? '✓' : '✗'}</span>
								</div>
							{/each}
						</div>
					{:else}
						<div class="text-muted" style="font-size:12px">No audit entries found.</div>
					{/if}
				{:else if mode === 'faces' && facesResult}
					<div class="faces-summary">
						<div class="hud-label" style="margin-bottom:8px">{facesResult.faceCount} FACE(S) DETECTED</div>
						{#each facesResult.faces as face}
							<div class="face-card">
								<span class="face-idx">#{face.faceIndex}</span>
								<span class="face-sentiment">{face.sentiment}</span>
								<span class="face-commentary">{face.commentary}</span>
							</div>
						{/each}
						<button class="hud-btn hud-btn-red" style="margin-top:12px;font-size:10px;padding:4px 12px"
							onclick={() => showFacesModal = true}>
							VIEW ANNOTATED IMAGE
						</button>
					</div>
				{:else if result}
					<pre class="result-pre">{JSON.stringify(result, null, 2)}</pre>
				{:else}
					<div class="text-muted" style="font-size:12px">No active assessment.</div>
				{/if}
			{/if}
		</div>
	</div>
</div>

{#if showFacesModal && facesResult}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div class="modal-backdrop" role="presentation" onclick={closeFacesModal}>
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="modal-box" role="dialog" aria-modal="true" tabindex="-1" onclick={(e) => e.stopPropagation()}>
			<div class="modal-header">
				<span class="hud-label" style="color:var(--hud-red)">FACE ANALYSIS — {facesResult.faceCount} TARGET(S)</span>
				<div style="display:flex;gap:8px;align-items:center">
					<a class="hud-btn download-btn" href={facesResult.imageUrl} download>DOWNLOAD</a>
					<button class="hud-btn close-btn" onclick={closeFacesModal}>✕</button>
				</div>
			</div>
			<div class="modal-body">
				<img src={facesResult.imageUrl} alt="Annotated" class="annotated-img" />
				<div class="faces-list">
					{#each facesResult.faces as face}
						<div class="face-card">
							<span class="face-idx">FACE #{face.faceIndex}</span>
							<span class="face-sentiment">{face.sentiment}</span>
							<p class="face-commentary">{face.commentary}</p>
						</div>
					{/each}
				</div>
			</div>
		</div>
	</div>
{/if}

<style>
	.page-layout { display: flex; flex-direction: column; height: 100%; }
	.grid-2 { display: grid; grid-template-columns: 380px 1fr; gap: 16px; flex: 1; min-height: 0; }
	.cam-grid { grid-template-columns: 280px 1fr; }
	.hud-panel { padding: 16px; }
	.panel-title { display: block; margin-bottom: 12px; }
	.mode-tabs { display: flex; gap: 6px; margin-bottom: 16px; }
	.mode-tab { font-size: 10px; padding: 4px 10px; }
	.mode-tab.active { background: var(--hud-red); color: var(--hud-bg); border-color: var(--hud-red); }
	.tab-description { font-size: 10px; color: var(--hud-cyan); letter-spacing: 0.08em; margin: -8px 0 12px; }
	.form { display: flex; flex-direction: column; gap: 12px; }
	.field { display: flex; flex-direction: column; gap: 4px; }
	.submit-btn { width: 100%; padding: 10px; margin-top: 4px; }
	.hud-btn-red { color: var(--hud-red); border-color: var(--hud-red); }
	.hud-btn-red:hover { background: var(--hud-red); color: var(--hud-bg); }
	.result-panel { overflow: auto; }
	.result-pre { font-size: 11px; color: var(--hud-text); white-space: pre-wrap; word-break: break-all; line-height: 1.5; }

	/* ── Camera controls ──────────────────────────────────────────── */
	.cam-controls {
		display: flex;
		flex-direction: column;
		gap: 12px;
		flex: 1;
	}
	.cam-actions { margin-top: auto; padding-top: 12px; }
	.cam-status {
		display: flex;
		align-items: center;
		gap: 6px;
		font-family: var(--font-mono);
		font-size: 10px;
		letter-spacing: 0.08em;
		color: #ffffff44;
	}
	.cam-status-active { color: var(--hud-red); }
	.cam-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: #ffffff22;
		flex-shrink: 0;
	}
	.cam-status-active .cam-dot {
		background: var(--hud-red);
		box-shadow: 0 0 6px var(--hud-red);
		animation: pulse 1.2s ease-in-out infinite;
	}
	@keyframes pulse {
		0%, 100% { opacity: 1; }
		50%       { opacity: 0.4; }
	}
	.cam-error {
		font-family: var(--font-mono);
		font-size: 11px;
		color: var(--hud-red);
		letter-spacing: 0.04em;
		line-height: 1.4;
	}
	.hud-btn-log { color: #ffaa00; border-color: #ffaa00; }
	.hud-btn-log:hover:not(:disabled) { background: #ffaa00; color: var(--hud-bg); }
	.hud-btn-log:disabled { opacity: 0.6; cursor: not-allowed; }

	.cam-stop-btn {
		width: 100%;
		color: #666;
		border-color: #444;
	}
	.cam-stop-btn:hover { background: #333; color: #fff; border-color: #666; }

	/* ── Camera result panel ──────────────────────────────────────── */
	.cam-result-panel {
		padding: 0;
		overflow: hidden;
		display: flex;
		flex-direction: column;
	}
	.cam-view {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		background: #000;
		position: relative;
		min-height: 0;
	}
	.cam-canvas {
		width: 100%;
		flex: 1;
		object-fit: contain;
		display: block;
		min-height: 0;
	}
	.cam-hidden { display: none; }
	.cam-placeholder {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 4px;
		opacity: 0.6;
	}
	.cam-placeholder-title {
		font-size: 20px;
		color: var(--hud-red);
		letter-spacing: 0.2em;
	}

	/* ── Detection sidebar ────────────────────────────────────────── */
	.detect-sidebar {
		background: #000a;
		border-top: 1px solid #ff2d5530;
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 10px;
		max-height: 220px;
		overflow-y: auto;
	}
	.detect-section { display: flex; flex-direction: column; gap: 4px; }
	.detect-title { font-size: 9px; letter-spacing: 0.12em; margin-bottom: 4px; }
	.detect-row {
		display: flex;
		justify-content: space-between;
		font-family: var(--font-mono);
		font-size: 10px;
		color: var(--hud-cyan);
	}
	.detect-class { letter-spacing: 0.05em; }
	.detect-conf { opacity: 0.7; }

	/* ── Claude Vision result ─────────────────────────────────────── */
	.vision-result { border-top: 1px solid #ff2d5520; padding-top: 8px; }
	.vision-level {
		font-family: var(--font-mono);
		font-size: 14px;
		font-weight: bold;
		letter-spacing: 0.12em;
	}
	.vision-conf { font-size: 10px; opacity: 0.7; }
	.vision-summary {
		font-family: var(--font-mono);
		font-size: 10px;
		color: var(--hud-text);
		line-height: 1.5;
		margin: 4px 0;
	}
	.vision-actions { display: flex; flex-direction: column; gap: 3px; }
	.vision-action {
		font-family: var(--font-mono);
		font-size: 10px;
		color: var(--hud-cyan);
		opacity: 0.8;
	}

	/* ── Cyan button variant ──────────────────────────────────────── */
	.hud-btn-cyan { color: var(--hud-cyan); border-color: var(--hud-cyan); }
	.hud-btn-cyan:hover:not(:disabled) { background: var(--hud-cyan); color: var(--hud-bg); }
	.hud-btn-cyan:disabled { opacity: 0.5; cursor: not-allowed; }

	/* ── File input ───────────────────────────────────────────────── */
	.hud-file-input { display: none; }
	.hud-file-label {
		display: block;
		padding: 6px 10px;
		border: 1px solid var(--hud-red);
		color: var(--hud-red);
		font-family: var(--font-mono);
		font-size: 11px;
		cursor: pointer;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		background: transparent;
	}
	.hud-file-label:hover { background: var(--hud-red); color: var(--hud-bg); }
	.preview-wrap { max-height: 400px; overflow: hidden; border: 1px solid #ff2d5540; background: #000; }
	.preview-img { width: 100%; max-height: 400px; object-fit: contain; display: block; }

	/* ── Faces ────────────────────────────────────────────────────── */
	.faces-summary { display: flex; flex-direction: column; gap: 8px; }
	.face-card { display: flex; flex-direction: column; gap: 2px; padding: 8px; border: 1px solid #00d4ff33; background: #00d4ff08; }
	.face-idx { font-size: 9px; color: #00d4ff88; font-family: var(--font-mono); letter-spacing: 0.1em; }
	.face-sentiment { font-size: 12px; color: #00d4ff; font-family: var(--font-mono); font-weight: bold; text-transform: uppercase; }
	.face-commentary { font-size: 11px; color: var(--hud-text); font-family: var(--font-mono); margin: 0; }

	/* ── Modal ────────────────────────────────────────────────────── */
	.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.85); display: flex; align-items: center; justify-content: center; z-index: 1000; }
	.modal-box { background: var(--hud-bg); border: 1px solid var(--hud-red); box-shadow: var(--glow-red); width: min(90vw, 800px); max-height: 90vh; display: flex; flex-direction: column; }
	.modal-header { display: flex; justify-content: space-between; align-items: center; padding: 12px 16px; border-bottom: 1px solid #ff2d5540; }
	.close-btn { padding: 2px 8px; font-size: 12px; }
	.modal-body { display: flex; gap: 16px; padding: 16px; overflow: auto; flex: 1; }
	.annotated-img { max-width: 60%; object-fit: contain; align-self: flex-start; border: 1px solid #00d4ff40; }
	.faces-list { flex: 1; display: flex; flex-direction: column; gap: 10px; overflow: auto; }
	.download-btn { font-size: 10px; padding: 2px 10px; text-decoration: none; color: var(--hud-red); border-color: var(--hud-red); }
	.download-btn:hover { background: var(--hud-red); color: var(--hud-bg); }

	/* ── Audit ────────────────────────────────────────────────────── */
	.status-badge { display: flex; align-items: center; gap: 12px; padding: 10px 14px; border: 1px solid currentColor; font-family: var(--font-mono); }
	.status-green  { color: #00ff88; border-color: #00ff8866; background: #00ff8808; }
	.status-yellow { color: #ffd700; border-color: #ffd70066; background: #ffd70008; }
	.status-red    { color: var(--hud-red); border-color: #ff2d5566; background: #ff2d5508; }
	.status-label  { font-size: 13px; font-weight: bold; letter-spacing: 0.1em; flex: 1; }
	.status-score  { font-size: 22px; font-weight: bold; }
	.status-color-label { font-size: 10px; opacity: 0.7; letter-spacing: 0.12em; }
	.audit-entries { display: flex; flex-direction: column; gap: 4px; overflow: auto; }
	.audit-row { display: grid; grid-template-columns: 1fr auto auto auto; gap: 8px; align-items: center; padding: 5px 8px; border-bottom: 1px solid #ffffff08; font-size: 10px; font-family: var(--font-mono); }
	.audit-row-threat { border-left: 2px solid var(--row-color, #666); background: color-mix(in srgb, var(--row-color, #666) 6%, transparent); }
	.audit-action  { color: var(--hud-text); letter-spacing: 0.04em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: flex; align-items: center; gap: 6px; }
	.audit-action-threat { color: var(--row-color, var(--hud-text)); }
	.threat-badge { font-size: 8px; padding: 1px 4px; border: 1px solid; border-radius: 2px; white-space: nowrap; flex-shrink: 0; }
	.audit-subject { color: #00d4ff88; white-space: nowrap; }
	.audit-ts      { color: #ffffff44; white-space: nowrap; }
	.audit-ok      { color: #00ff88; }
	.audit-fail    { color: var(--hud-red); }

	/* ── Scan bar ─────────────────────────────────────────────────── */
	.scan-wrap { display: flex; flex-direction: column; gap: 12px; padding-top: 8px; }
	.scan-bar { height: 2px; background: var(--hud-cyan); animation: scan-sweep 1.2s ease-in-out infinite; transform-origin: left; }
	@keyframes scan-sweep { 0% { transform: scaleX(0); opacity: 1; } 60% { transform: scaleX(1); opacity: 1; } 100% { transform: scaleX(1); opacity: 0; } }
	.scan-label { font-size: 10px; opacity: 0.6; animation: blink 1.2s step-end infinite; }
	@keyframes blink { 0%, 100% { opacity: 0.6; } 50% { opacity: 0.2; } }
</style>
