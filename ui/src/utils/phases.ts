// Phase text for the two background state machines the daemon reports: the
// runtime download and the connection-change operations.

import type { DownloadStatus, Operation } from '@/api/types'

// 下载阶段文案，与后端阶段名一一对应。
const downloadPhases: Record<string, string> = {
	queued: '正在等待开始',
	release: '正在读取稳定版本信息',
	metadata: '正在核对版本信息',
	architecture: '正在确认处理器架构',
	storage: '正在检查可用存储空间',
	download: '正在下载运行时',
	verify: '正在校验下载内容',
	extract: '正在解压运行时',
	validate: '正在验证运行时',
	install: '正在安装运行时',
	restart: '正在重启连接服务',
	done: '运行时安装完成',
	interrupted: '上次安装被中断',
}

/** downloadMessage renders one line describing the runtime download. */
export function downloadMessage(download?: DownloadStatus | null): string {
	const download_ = download || {}
	const message = downloadPhases[download_.phase || ''] || (download_.state === 'running' ? '正在准备…' : '')
	if (download_.state === 'failed') {
		return download_.message || (message ? `安装未完成：${message}` : '安装未完成。')
	}
	return message
}

// 后台操作阶段文案。
const operationPhases: Record<string, string> = {
	queued: '正在排队执行本机设置变更…',
	console: '正在核对 Console 账号与注册密钥…',
	release: '正在读取 EasyTier 连接配置…',
	service: '正在启动服务并检查连接…',
	network: '正在向 Console 更新网络成员…',
	complete: '本机设置已完成。',
}

/** operationMessage renders the running state of a connection change. */
export function operationMessage(operation?: Operation | null): string {
	return operationPhases[(operation && operation.phase) || ''] || '正在执行本机设置变更…'
}

/** percentOf clamps a reported progress value into 0..100 and rejects NaN. */
export function percentOf(value: unknown): number {
	const parsed = Number(value)
	if (!Number.isFinite(parsed)) {
		return 0
	}
	return Math.max(0, Math.min(100, parsed))
}
