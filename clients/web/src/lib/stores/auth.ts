import { writable, derived } from 'svelte/store';

interface AuthState {
	token: string | null;
	subjectId: string | null;
	userId: string | null;
	role: string | null;
}

function createAuthStore() {
	const stored = typeof localStorage !== 'undefined'
		? localStorage.getItem('jarvis_auth')
		: null;
	const initial: AuthState = stored ? JSON.parse(stored) : { token: null, subjectId: null, userId: null, role: null };

	const { subscribe, set, update } = writable<AuthState>(initial);

	return {
		subscribe,
		login(token: string, subjectId: string, userId: string, role: string) {
			const state = { token, subjectId, userId, role };
			if (typeof localStorage !== 'undefined') {
				localStorage.setItem('jarvis_auth', JSON.stringify(state));
			}
			set(state);
		},
		logout() {
			if (typeof localStorage !== 'undefined') {
				localStorage.removeItem('jarvis_auth');
			}
			set({ token: null, subjectId: null, userId: null, role: null });
		}
	};
}

export const auth = createAuthStore();
export const isAuthenticated = derived(auth, ($auth) => !!$auth.token);
export const token = derived(auth, ($auth) => $auth.token);
export const subjectId = derived(auth, ($auth) => $auth.subjectId);
export const userId = derived(auth, ($auth) => $auth.userId);
export const role = derived(auth, ($auth) => $auth.role);
export const isAdmin = derived(auth, ($auth) => $auth.role === 'ROLE_ADMIN');
export const isEditor = derived(auth, ($auth) => $auth.role === 'ROLE_EDITOR');
export const canManageSprints = derived(auth, ($auth) => $auth.role === 'ROLE_ADMIN' || $auth.role === 'ROLE_EDITOR');
