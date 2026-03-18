import { writable, derived } from 'svelte/store';

interface AuthState {
	token: string | null;
	subjectId: string | null;
}

function createAuthStore() {
	const stored = typeof localStorage !== 'undefined'
		? localStorage.getItem('jarvis_auth')
		: null;
	const initial: AuthState = stored ? JSON.parse(stored) : { token: null, subjectId: null };

	const { subscribe, set, update } = writable<AuthState>(initial);

	return {
		subscribe,
		login(token: string, subjectId: string) {
			const state = { token, subjectId };
			if (typeof localStorage !== 'undefined') {
				localStorage.setItem('jarvis_auth', JSON.stringify(state));
			}
			set(state);
		},
		logout() {
			if (typeof localStorage !== 'undefined') {
				localStorage.removeItem('jarvis_auth');
			}
			set({ token: null, subjectId: null });
		}
	};
}

export const auth = createAuthStore();
export const isAuthenticated = derived(auth, ($auth) => !!$auth.token);
export const token = derived(auth, ($auth) => $auth.token);
export const subjectId = derived(auth, ($auth) => $auth.subjectId);
