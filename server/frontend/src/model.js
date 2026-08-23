import request from '@/request';

/**
 * 获取模型列表
 * @param {Object} params - 查询参数
 * @param {number} params.page - 页码
 * @param {number} params.pageSize - 每页数量
 * @param {string} params.type - 资产类型
 * @param {string} params.category - 分类
 * @param {string} params.tags - 标签
 * @param {string} params.name - 名称（模糊搜索）
 */
export function getModelList(params) {
    return request({
        url: `/model/list`,
        method: 'GET',
        params
    });
}

/**
 * 获取模型详情
 * @param {string} uuid - 模型UUID
 * @param {string} version - 版本号（可选）
 */
export function getModelDetail(uuid, version) {
    return request({
        url: `/model/detail/${uuid}`,
        method: 'GET',
        params: version ? { version } : {}
    });
}

/**
 * 获取模型缩略图URL
 * @param {string} uuid - 模型UUID
 * @param {string} size - 尺寸（64x64 或 256x256）
 * @param {string} version - 版本号（可选）
 */
export function getThumbnailUrl(uuid, size = '256x256', version) {
    const baseUrl = window.location.pathname.slice(0, location.pathname.lastIndexOf("/"))
        .replace("/static", "") + "/api";
    let url = `${baseUrl}/model/thumbnail/${uuid}/${size}`;
    if (version) {
        url += `?version=${version}`;
    }
    return url;
}

/**
 * 下载模型
 * @param {string} uuid - 模型UUID
 * @param {string} version - 版本号（可选）
 */
export function downloadModel(uuid, version) {
    return request({
        url: `/model/download/${uuid}`,
        method: 'GET',
        params: version ? { version } : {},
        responseType: 'blob'
    });
}

/**
 * 上传模型
 * @param {FormData} formData - 包含文件的FormData对象
 * @param {Function} onProgress - 上传进度回调
 */
export function uploadModel(formData, onProgress) {
    return request({
        url: `/model/upload`,
        method: 'POST',
        data: formData,
        headers: {
            'Content-Type': 'multipart/form-data'
        },
        onUploadProgress: (progressEvent) => {
            if (onProgress && progressEvent.total) {
                const percentCompleted = Math.round((progressEvent.loaded * 100) / progressEvent.total);
                onProgress(percentCompleted);
            }
        }
    });
}

/**
 * 删除模型
 * @param {string} uuid - 模型UUID
 * @param {string} version - 版本号（可选，不传则删除整个模型）
 */
export function deleteModel(uuid, version) {
    return request({
        url: `/model/delete/${uuid}`,
        method: 'DELETE',
        params: version ? { version } : {}
    });
}

/**
 * 获取模型的所有版本
 * @param {string} uuid - 模型UUID
 */
export function getModelVersions(uuid) {
    return request({
        url: `/model/versions/${uuid}`,
        method: 'GET'
    });
}

/**
 * 格式化文件大小
 * @param {number} bytes - 字节数
 */
export function formatFileSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

/**
 * 格式化日期时间
 * @param {string} dateString - 日期字符串
 */
export function formatDateTime(dateString) {
    if (!dateString) return '';
    const date = new Date(dateString);
    return date.toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
}
