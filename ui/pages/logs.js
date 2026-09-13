// Diagnostics page: connection status, service controls, and recent daemon logs.
import {
	h, card, detailList, button, banner, notify,
} from '../lib/dom.js';
import { api, ApiError, needsRelogin } from '../lib/api.js';

const LINE_OPTIONS = [ 100, 200, 500 ];
const EMPTY_LOGS = '暂无匹配的日志';

// lineCount survives navigation: the shell unmounts the page but the module
// stays loaded, so the selected line count is kept here.
let lineCount = 200;

// Per-mount state. Every DOM handle below belongs to the latest render().
let mounted = false;
let view = null;
let statusSlot = null;
let logOutput = null;
let refreshButton = null;
let actionButtons = [];
let actionInFlight = false;
let errorBanner = null;
let timers = new Set();

/** friendlyMessage maps any failure to a user-facing Chinese sentence. */
function friendlyMessage(error) {
	if (needsRelogin(error)) {
		return '登录状态已失效，请重新登录 DSM 后再试。';
	}
	return error && error.message ? error.message : '操作失败，请稍后重试。';
}

function showError(error) {
	const message = friendlyMessage(error);
	clearError();
	if (mounted && view) {
		errorBanner = banner(message, 'error');
		view.prepend(errorBanner);
	}
	notify(message, 'error');
}

function clearError() {
	if (errorBanner) {
		errorBanner.remove();
		errorBanner = null;
	}
}

/** later schedules a callback and tracks it so unmount() can cancel it. */
function later(callback, delay) {
	const id = window.setTimeout(() => {
		timers.delete(id);
		callback();
	}, delay);
	timers.add(id);
	return id;
}

/** settle resolves {value} or {error}; tolerated errors resolve {value: null}. */
function settle(promise, tolerate) {
	return promise.then(
		(value) => ({ value, error: null }),
		(error) => (tolerate && tolerate(error) ? { value: null, error: null } : { value: null, error }),
	);
}

function isServiceStopped(error) {
	return error instanceof ApiError && error.code === 'service_not_running';
}

function interfacesOf(summary) {
	if (!summary || !Array.isArray(summary.interfaces)) {
		return [];
	}
	return summary.interfaces.filter(Boolean);
}

function setActionButtonsDisabled(disabled) {
	for (const item of actionButtons) {
		item.disabled = disabled;
	}
}

/** buildStatusCard renders the status card from the latest known state. */
function buildStatusCard(status, interfaces) {
	const running = Boolean(status && status.running);
	const machineID = status && status.machine_id;
	const restartButton = button('重启连接', {
		variant: 'primary',
		disabled: actionInFlight,
		onclick: () => handleServiceAction('restart'),
	});
	const toggleButton = running
		? button('停止连接', { variant: 'danger', disabled: actionInFlight, onclick: () => handleServiceAction('stop') })
		: button('启动连接', { variant: 'primary', disabled: actionInFlight, onclick: () => handleServiceAction('start') });
	actionButtons = [ restartButton, toggleButton ];
	return card('连接状态', [
		detailList([
			[ '连接服务', running ? '运行中' : '已停止' ],
			[ '隧道接口', interfaces.length > 0 ? interfaces.join(', ') : '暂无' ],
			[ 'Core 版本', status ? status.core_version : '' ],
			[ 'CLI 版本', status ? status.cli_version : '' ],
			[ '本机 ID', machineID ? h('span', { class: 'mono', text: machineID }) : '' ],
			[ '工作空间', status ? status.workspace_id : '' ],
			[ '配置服务器', status ? status.config_server : '' ],
		]),
		h('div', { class: 'row' }, actionButtons),
	], '连接异常时，可先核对下面的状态，再尝试重启连接。');
}

/** refreshStatus re-reads status/summary and swaps the status card in place. */
async function refreshStatus() {
	const [ statusResult, summaryResult ] = await Promise.all([
		settle(api.status()),
		settle(api.localSummary(), isServiceStopped),
	]);
	if (!mounted || !statusSlot) {
		return;
	}
	const error = statusResult.error || summaryResult.error;
	if (error) {
		showError(error);
	}
	actionInFlight = false;
	const next = buildStatusCard(statusResult.value, interfacesOf(summaryResult.value));
	statusSlot.replaceWith(next);
	statusSlot = next;
}

async function handleServiceAction(action) {
	if (actionInFlight) {
		return;
	}
	actionInFlight = true;
	setActionButtonsDisabled(true);
	try {
		await api.serviceAction(action);
		clearError();
		if (action === 'stop') {
			notify('连接已停止。', 'success');
		} else if (action === 'start') {
			notify('连接已启动，正在刷新状态…', 'success');
		} else {
			notify('连接服务已重启，正在刷新状态…', 'success');
		}
		// Give the service a moment to come up before re-reading its state.
		later(() => {
			refreshStatus();
		}, 1200);
	} catch (error) {
		actionInFlight = false;
		if (mounted) {
			setActionButtonsDisabled(false);
			showError(error);
			if (needsRelogin(error)) {
				refreshStatus();
			}
		}
	}
}

function renderLogText(text) {
	if (!logOutput) {
		return;
	}
	const trimmed = (text || '').trim();
	logOutput.textContent = trimmed === '' ? EMPTY_LOGS : text;
}

/** refreshLogs re-fetches the daemon log with the selected line count. */
async function refreshLogs() {
	if (!mounted) {
		return;
	}
	if (refreshButton) {
		refreshButton.disabled = true;
	}
	try {
		const result = await api.logs(lineCount);
		if (!mounted) {
			return;
		}
		clearError();
		renderLogText(result.logs || '');
	} catch (error) {
		if (mounted) {
			showError(error);
			if (logOutput && logOutput.textContent.trim() === '') {
				logOutput.textContent = '日志加载失败，请点击“刷新”重试。';
			}
		}
	} finally {
		if (mounted && refreshButton) {
			refreshButton.disabled = false;
		}
	}
}

/** buildLogCard renders the log card; initialLogs may be null on load failure. */
function buildLogCard(initialLogs) {
	const select = h('select', {
		'aria-label': '显示行数',
		onchange: (event) => {
			const value = Number(event.target.value);
			lineCount = LINE_OPTIONS.includes(value) ? value : 200;
		},
	}, LINE_OPTIONS.map((option) => h('option', {
		value: String(option),
		text: `${option} 行`,
		selected: option === lineCount,
	})));
	refreshButton = button('刷新', { onclick: () => refreshLogs() });
	logOutput = h('pre', { class: 'log-output' });
	renderLogText(initialLogs === null ? '日志加载失败，请点击“刷新”重试。' : initialLogs);
	return card('最近日志', [
		h('div', { class: 'row' }, [
			h('span', { class: 'muted', text: '显示行数' }),
			select,
			refreshButton,
		]),
		logOutput,
	], '连接出问题时，可在这里查看本机服务的最近记录。');
}

function buildPrivacyDetails() {
	return h('section', { class: 'card' }, [
		h('div', { class: 'card-body' }, [
			h('details', {}, [
				h('summary', { text: '关于诊断数据' }),
				h('p', {
					class: 'muted',
					text: '日志最多显示最近 64 KB 的内容。设备注册令牌、授权请求头和类似令牌的内容会在发送到浏览器之前自动移除，不会显示在这里。',
				}),
			]),
		]),
	]);
}

export async function render() {
	unmount();
	mounted = true;
	const [ statusResult, summaryResult, logsResult ] = await Promise.all([
		settle(api.status()),
		settle(api.localSummary(), isServiceStopped),
		settle(api.logs(lineCount)),
	]);
	statusSlot = buildStatusCard(statusResult.value, interfacesOf(summaryResult.value));
	view = h('div', { class: 'page' }, [
		statusSlot,
		buildLogCard(logsResult.error ? null : logsResult.value.logs || ''),
		buildPrivacyDetails(),
	]);
	// Surface the first load failure once, after the view exists so the banner
	// has somewhere to go.
	const loadError = statusResult.error || summaryResult.error || logsResult.error;
	if (loadError) {
		showError(loadError);
	}
	return view;
}

export function unmount() {
	mounted = false;
	for (const id of timers) {
		window.clearTimeout(id);
	}
	timers = new Set();
	view = null;
	statusSlot = null;
	logOutput = null;
	refreshButton = null;
	actionButtons = [];
	actionInFlight = false;
	errorBanner = null;
}
