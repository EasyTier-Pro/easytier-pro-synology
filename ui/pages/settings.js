// Settings page: everyday options plus advanced Console endpoints, loaded from
// and saved through the local API.
import { h, card, banner, button, notify } from '../lib/dom.js';
import { api, ApiError, needsRelogin } from '../lib/api.js';

/** messageOf renders the user-facing text for any thrown error. */
function messageOf(error) {
	if (error instanceof ApiError) {
		return error.message;
	}
	return '操作失败，请稍后重试。';
}

/** trimValue normalizes a text input value. */
function trimValue(input) {
	return input.value.trim();
}

/**
 * validateConsoleURL accepts only http:// or https:// addresses without
 * whitespace, mirroring the daemon-side rule.
 */
function validateConsoleURL(value) {
	if (value === '') {
		return '请输入 Console 地址。';
	}
	if (/\s/.test(value)) {
		return 'Console 地址不能包含空格，请输入完整的 http:// 或 https:// 地址。';
	}
	let parsed;
	try {
		parsed = new URL(value);
	} catch (error) {
		return 'Console 地址格式不正确，请输入完整的 http:// 或 https:// 地址，例如 https://api.console.easytier.net。';
	}
	if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
		return 'Console 地址必须以 http:// 或 https:// 开头。';
	}
	if (parsed.hostname === '') {
		return 'Console 地址缺少服务器名称，请输入完整的 http:// 或 https:// 地址。';
	}
	return '';
}

/**
 * validateConfigServer accepts an empty value (filled in automatically after
 * connecting) or an address with a scheme separator such as tcp://host:22020.
 */
function validateConfigServer(value) {
	if (value === '') {
		return '';
	}
	if (/\s/.test(value)) {
		return '配置服务器地址不能包含空格。';
	}
	if (!/^[A-Za-z][A-Za-z0-9+.-]*:\/\/\S+$/.test(value)) {
		return '配置服务器地址需要以「协议://服务器地址」的格式填写，例如 tcp://example.com:22020；不确定时请留空，连接成功后会自动填入。';
	}
	return '';
}

export async function render() {
	const status = await api.status();

	const root = h('div', { class: 'page' });
	const bannerSlot = h('div');
	root.appendChild(bannerSlot);

	const enabledInput = h('input', { type: 'checkbox' });
	enabledInput.checked = status.enabled === true;

	const consoleURLInput = h('input', {
		type: 'text',
		value: status.console_url || '',
		placeholder: 'https://api.console.easytier.net',
		spellcheck: 'false',
	});
	const insecureInput = h('input', { type: 'checkbox' });
	insecureInput.checked = status.allow_insecure_console === true;
	const configServerInput = h('input', {
		type: 'text',
		value: status.config_server || '',
		placeholder: 'tcp://example.com:22020',
		spellcheck: 'false',
	});

	const everydayCard = card('日常', [
		h('div', { class: 'field' }, [
			h('label', { class: 'checkbox' }, [
				enabledInput,
				h('span', { text: '开机自动连接' }),
			]),
			h('small', { text: '群晖开机后自动重新接入 EasyTier 虚拟网络。' }),
		]),
	], '日常使用只需要保留默认设置。');

	const advancedCard = card('高级', [
		h('div', { class: 'field' }, [
			h('label', { text: 'Console 地址' }),
			consoleURLInput,
			h('small', { text: 'EasyTier Console 的服务地址，一般以 https:// 开头。' }),
		]),
		h('div', { class: 'field' }, [
			h('label', { class: 'checkbox' }, [
				insecureInput,
				h('span', { text: '允许使用不安全的 HTTP 地址' }),
			]),
			h('small', { text: '仅在 Console 地址为 http:// 时需要开启；自己搭建 Console 时才可能用到，正常情况下请保持关闭。' }),
		]),
		h('div', { class: 'field' }, [
			h('label', { text: '配置服务器地址' }),
			configServerInput,
			h('small', { text: '留空即可：成功连接后会自动填入；需要手动填写时请使用「协议://服务器地址」的格式，例如 tcp://example.com:22020。' }),
		]),
		h('div', { class: 'field' }, [
			h('label', { text: '运行时安装目录' }),
			h('div', { class: 'mono', text: status.install_dir || '—' }),
			h('small', { text: '运行时文件的安装目录由 DSM 统一管理，无需手动修改。' }),
		]),
	], '除非清楚自己在做什么，否则请保持默认。');

	const saveButton = button('保存', { variant: 'primary' });
	const actionsCard = card(null, [
		h('div', { class: 'row' }, [ saveButton ]),
		h('p', { class: 'muted', text: '虚拟网卡对局域网的访问请在 DSM 控制面板 → 安全性 → 防火墙 中放行。' }),
	]);

	root.appendChild(everydayCard);
	root.appendChild(advancedCard);
	root.appendChild(actionsCard);

	let saving = false;

	/** reloadForm replaces the fields with the latest saved values. */
	async function reloadForm() {
		try {
			const latest = await api.status();
			enabledInput.checked = latest.enabled === true;
			consoleURLInput.value = latest.console_url || '';
			insecureInput.checked = latest.allow_insecure_console === true;
			configServerInput.value = latest.config_server || '';
		} catch (error) {
			// Reloading is best-effort; the saved values are already applied.
		}
	}

	/** save validates the form, submits it and reports the outcome. */
	async function save() {
		if (saving) {
			return;
		}
		const values = {
			enabled: enabledInput.checked,
			console_url: trimValue(consoleURLInput),
			allow_insecure_console: insecureInput.checked,
			config_server: trimValue(configServerInput),
		};
		const consoleURLError = validateConsoleURL(values.console_url);
		const configServerError = validateConfigServer(values.config_server);
		if (consoleURLError || configServerError) {
			bannerSlot.replaceChildren(banner(consoleURLError || configServerError, 'error'));
			return;
		}
		saving = true;
		saveButton.disabled = true;
		bannerSlot.replaceChildren();
		try {
			await api.applySettings(values);
			bannerSlot.replaceChildren();
			notify('设置已保存。', 'success');
			await reloadForm();
		} catch (error) {
			if (needsRelogin(error)) {
				bannerSlot.replaceChildren(banner('DSM 登录已失效，请重新登录 DSM 后再保存设置。', 'error'));
			} else {
				bannerSlot.replaceChildren(banner(messageOf(error), 'error'));
			}
		} finally {
			saving = false;
			saveButton.disabled = false;
		}
	}

	saveButton.addEventListener('click', save);
	return root;
}
