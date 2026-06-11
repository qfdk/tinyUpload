/**
 * TinyUpload - 文件上传客户端
 * 简化流程：整页拖拽 / 点击上传区即选文件 / 上传成功自动复制链接
 * 多语言：中文 / English / Français
 */

const I18N = {
    zh: {
        tagline: '简单上传 · Simple Is Beautiful',
        dropHint: '拖拽、粘贴（⌘V），或点击上传',
        uploadSuccess: '上传成功',
        copyInfo: '复制信息',
        cliTitle: '命令行用法',
        cliUpload: '上传',
        cliDownload: '下载',
        cliDelete: '删除',
        fileWord: '文件名',
        codeWord: '删除码',
        filesTitle: '已上传的文件',
        download: '下载',
        delete: '删除',
        fileLink: '文件链接',
        deleteCode: '删除码',
        preparing: '准备上传...',
        progress: '已上传 {loaded} / {total}',
        cancel: '取消',
        confirmDelete: '确定要删除这个文件吗？',
        confirmYes: '删除',
        confirmNo: '取消',
        emptySkipped: '文件 "{name}" 为空，已跳过',
        tooLarge: '文件 "{name}" 超过大小限制，已跳过',
        uploadingN: '上传中 {i}/{n}: {name}',
        busy: '正在上传中，请稍候...',
        uploadFailed: '上传失败: {msg}',
        serverFormatError: '服务器响应格式错误',
        httpError: '上传失败: {status}',
        networkError: '网络错误',
        uploadCanceled: '上传已取消',
        deleted: '文件已删除',
        deletedGone: '文件已不可用，已从列表移除',
        deleteFailed: '删除失败: {msg}',
        copied: '复制成功！',
        copyFailed: '复制失败',
        linkCopied: '链接已自动复制',
    },
    en: {
        tagline: 'Simple file sharing',
        dropHint: 'Drag & drop, paste (⌘V), or click to upload',
        uploadSuccess: 'Uploaded',
        copyInfo: 'Copy info',
        cliTitle: 'Command line',
        cliUpload: 'Upload',
        cliDownload: 'Download',
        cliDelete: 'Delete',
        fileWord: 'file',
        codeWord: 'code',
        filesTitle: 'Uploaded files',
        download: 'Download',
        delete: 'Delete',
        fileLink: 'Link',
        deleteCode: 'Delete code',
        preparing: 'Preparing…',
        progress: 'Uploaded {loaded} / {total}',
        cancel: 'Cancel',
        confirmDelete: 'Delete this file?',
        confirmYes: 'Delete',
        confirmNo: 'Cancel',
        emptySkipped: '"{name}" is empty, skipped',
        tooLarge: '"{name}" exceeds the size limit, skipped',
        uploadingN: 'Uploading {i}/{n}: {name}',
        busy: 'Upload in progress, please wait…',
        uploadFailed: 'Upload failed: {msg}',
        serverFormatError: 'Invalid server response',
        httpError: 'Upload failed: {status}',
        networkError: 'Network error',
        uploadCanceled: 'Upload canceled',
        deleted: 'File deleted',
        deletedGone: 'File no longer available, removed from list',
        deleteFailed: 'Delete failed: {msg}',
        copied: 'Copied!',
        copyFailed: 'Copy failed',
        linkCopied: 'Link copied to clipboard',
    },
    fr: {
        tagline: 'Partage de fichiers, tout simplement',
        dropHint: 'Glissez-déposez, collez (⌘V) ou cliquez pour téléverser',
        uploadSuccess: 'Téléversement réussi',
        copyInfo: 'Copier les infos',
        cliTitle: 'Ligne de commande',
        cliUpload: 'Téléverser',
        cliDownload: 'Télécharger',
        cliDelete: 'Supprimer',
        fileWord: 'fichier',
        codeWord: 'code',
        filesTitle: 'Fichiers téléversés',
        download: 'Télécharger',
        delete: 'Supprimer',
        fileLink: 'Lien',
        deleteCode: 'Code de suppression',
        preparing: 'Préparation…',
        progress: '{loaded} / {total} téléversés',
        cancel: 'Annuler',
        confirmDelete: 'Supprimer ce fichier ?',
        confirmYes: 'Supprimer',
        confirmNo: 'Annuler',
        emptySkipped: 'Le fichier « {name} » est vide, ignoré',
        tooLarge: 'Le fichier « {name} » dépasse la taille maximale, ignoré',
        uploadingN: 'Téléversement {i}/{n} : {name}',
        busy: 'Téléversement en cours, veuillez patienter…',
        uploadFailed: 'Échec du téléversement : {msg}',
        serverFormatError: 'Réponse du serveur invalide',
        httpError: 'Échec du téléversement : {status}',
        networkError: 'Erreur réseau',
        uploadCanceled: 'Téléversement annulé',
        deleted: 'Fichier supprimé',
        deletedGone: 'Fichier indisponible, retiré de la liste',
        deleteFailed: 'Échec de la suppression : {msg}',
        copied: 'Copié !',
        copyFailed: 'Échec de la copie',
        linkCopied: 'Lien copié automatiquement',
    },
};

const HTML_LANG = { zh: 'zh-CN', en: 'en', fr: 'fr' };

class Lang {
    constructor() {
        this.current = this.detect();
    }

    detect() {
        const saved = localStorage.getItem('lang');
        if (saved && I18N[saved]) return saved;
        const nav = (navigator.language || 'en').toLowerCase();
        if (nav.startsWith('zh')) return 'zh';
        if (nav.startsWith('fr')) return 'fr';
        return 'en';
    }

    set(lang) {
        if (!I18N[lang]) return;
        this.current = lang;
        try {
            localStorage.setItem('lang', lang);
        } catch (e) { /* 隐私模式下忽略 */ }
    }

    t(key, params) {
        let text = I18N[this.current][key] || I18N.en[key] || key;
        if (params) {
            for (const [k, v] of Object.entries(params)) {
                text = text.replace(`{${k}}`, v);
            }
        }
        return text;
    }
}

class TinyUpload {
    constructor() {
        this.baseUrl = TinyUpload.buildBaseUrl();
        this.state = {
            isUploading: false,
            uploadController: null,
            currentResults: []
        };

        this.lang = new Lang();
        this.dom = this.initDOM();
        this.storage = new StorageManager();
        this.ui = new UIManager(this.dom, this.lang);

        this.init();
    }

    // 对外分享的链接一律用 https；本机/内网地址保持原协议方便开发调试
    static buildBaseUrl() {
        const { protocol, host, hostname } = window.location;
        const isLocal = hostname === 'localhost'
            || hostname === '[::1]'
            || /^127\./.test(hostname)
            || /^10\./.test(hostname)
            || /^192\.168\./.test(hostname)
            || /^172\.(1[6-9]|2\d|3[01])\./.test(hostname);
        if (protocol === 'http:' && !isLocal) {
            return `https://${host}`;
        }
        return window.location.origin;
    }

    initDOM() {
        return {
            dropZone: document.getElementById('dropZone'),
            fileInput: document.getElementById('fileInput'),
            uploadProgress: document.getElementById('uploadProgress'),
            progressBar: document.querySelector('.progress-bar-fill'),
            statusText: document.querySelector('.status-text'),
            cancelButton: document.getElementById('cancelButton'),
            uploadResult: document.getElementById('uploadResult'),
            resultContent: document.getElementById('resultContent'),
            resultClose: document.getElementById('resultClose'),
            fileList: document.getElementById('fileList'),
            copyButton: document.getElementById('copyButton'),
            langSwitch: document.getElementById('langSwitch'),
            themeToggle: document.getElementById('themeToggle'),
            cliCodes: {
                upload: document.getElementById('uploadCommand'),
                download: document.getElementById('downloadCommand'),
                delete: document.getElementById('deleteCommand'),
            }
        };
    }

    init() {
        this.applyLanguage();
        this.updateThemeIcon();
        this.setupEventListeners();
        this.loadFileList();
        this.setupPasteUpload();
    }

    // ---- 主题 ----

    toggleTheme() {
        const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = next;
        try {
            localStorage.setItem('theme', next);
        } catch (e) { /* 隐私模式下忽略 */ }
        this.updateThemeIcon();
    }

    updateThemeIcon() {
        const isDark = document.documentElement.dataset.theme === 'dark';
        this.dom.themeToggle.textContent = isDark ? '☀️' : '🌙';
    }

    // ---- 多语言 ----

    applyLanguage() {
        const t = (k, p) => this.lang.t(k, p);
        document.documentElement.lang = HTML_LANG[this.lang.current];

        document.querySelectorAll('[data-i18n]').forEach(el => {
            el.textContent = t(el.dataset.i18n);
        });

        // CLI 命令用当前页面地址实时生成
        const host = window.location.host;
        const origin = this.baseUrl;
        const file = t('fileWord');
        const code = t('codeWord');
        this.dom.cliCodes.upload.textContent = `curl -T ${file} ${host}`;
        this.dom.cliCodes.download.textContent = `curl -O ${origin}/xxxx/${file}`;
        this.dom.cliCodes.delete.textContent = `curl -X DELETE "${origin}/delete/xxxx/${file}?code=${code}"`;

        this.dom.langSwitch.querySelectorAll('button').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.lang === this.lang.current);
        });
    }

    switchLanguage(lang) {
        this.lang.set(lang);
        this.applyLanguage();
        this.loadFileList();
        if (!this.dom.uploadResult.hidden && this.state.currentResults.length > 0) {
            this.ui.showUploadResult(this.state.currentResults, this.baseUrl);
        }
    }

    // ---- 事件 ----

    setupEventListeners() {
        // 点击上传区任意位置选择文件
        this.dom.dropZone.addEventListener('click', (e) => {
            if (e.target.closest('#uploadProgress') || e.target === this.dom.fileInput) return;
            this.dom.fileInput.click();
        });
        this.dom.fileInput.addEventListener('change', (e) => this.handleFileInputChange(e));

        this.dom.cancelButton.addEventListener('click', (e) => {
            e.stopPropagation();
            this.cancelUpload();
        });

        this.dom.copyButton.addEventListener('click', () => this.copyResult());
        this.dom.resultClose.addEventListener('click', () => this.ui.hideUploadResult());

        this.dom.themeToggle.addEventListener('click', () => this.toggleTheme());

        this.dom.langSwitch.addEventListener('click', (e) => {
            const btn = e.target.closest('button[data-lang]');
            if (btn) this.switchLanguage(btn.dataset.lang);
        });

        // CLI 命令点击即复制
        Object.values(this.dom.cliCodes).forEach(codeEl => {
            codeEl.addEventListener('click', () => this.copyText(codeEl.textContent));
            codeEl.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') this.copyText(codeEl.textContent);
            });
        });

        this.setupDragAndDrop();
    }

    setupPasteUpload() {
        // ⌘V / Ctrl+V 粘贴直接上传：文件、截图，或剪贴板纯文本（存为 paste.txt）
        document.addEventListener('paste', async (e) => {
            const cd = e.clipboardData;
            if (!cd) return;

            const files = Array.from(cd.files);
            if (files.length > 0) {
                e.preventDefault();
                await this.handleFiles(files);
                return;
            }

            const text = cd.getData('text');
            if (text && text.trim()) {
                e.preventDefault();
                const file = new File([text], 'paste.txt', { type: 'text/plain' });
                await this.handleFiles([file]);
            }
        });
    }

    setupDragAndDrop() {
        // 整页都是拖放目标，视觉反馈落在上传区
        let dragCounter = 0;

        document.addEventListener('dragenter', (e) => {
            e.preventDefault();
            dragCounter++;
            this.dom.dropZone.classList.add('dragover');
        });

        document.addEventListener('dragleave', (e) => {
            e.preventDefault();
            dragCounter--;
            if (dragCounter === 0) {
                this.dom.dropZone.classList.remove('dragover');
            }
        });

        document.addEventListener('dragover', (e) => {
            e.preventDefault();
        });

        document.addEventListener('drop', async (e) => {
            e.preventDefault();
            dragCounter = 0;
            this.dom.dropZone.classList.remove('dragover');

            const files = Array.from(e.dataTransfer.files);
            if (files.length > 0) {
                await this.handleFiles(files);
            }
        });
    }

    async handleFileInputChange(e) {
        if (e.target.files.length > 0) {
            await this.handleFiles(Array.from(e.target.files));
            e.target.value = '';
        }
    }

    // ---- 上传 ----

    async handleFiles(files) {
        const validFiles = this.validateFiles(files);
        if (validFiles.length === 0) return;

        if (validFiles.length === 1) {
            await this.handleFile(validFiles[0]);
        } else {
            await this.handleMultipleFiles(validFiles);
        }
    }

    validateFiles(files) {
        const maxSize = 1024 * 1024 * 1024; // 1GB
        const validFiles = [];

        for (const file of files) {
            if (file.size === 0) {
                this.ui.showToast(this.lang.t('emptySkipped', { name: file.name }));
                continue;
            }

            if (file.size > maxSize) {
                this.ui.showToast(this.lang.t('tooLarge', { name: file.name }));
                continue;
            }

            validFiles.push(file);
        }

        return validFiles;
    }

    async handleFile(file) {
        const result = await this.uploadAndStore(file);
        if (result) {
            this.state.currentResults = [result];
            this.ui.showUploadResult([result], this.baseUrl);
            await this.loadFileList();
            // 简化流程：单文件上传成功后自动复制链接
            this.autoCopyLink(result);
        }
    }

    async handleMultipleFiles(files) {
        const results = [];
        for (let i = 0; i < files.length; i++) {
            const file = files[i];
            this.ui.showToast(this.lang.t('uploadingN', { i: i + 1, n: files.length, name: file.name }));
            const r = await this.uploadAndStore(file);
            if (r) results.push(r);
        }
        if (results.length > 0) {
            this.state.currentResults = results;
            this.ui.showUploadResult(results, this.baseUrl);
            await this.loadFileList();
        }
    }

    async autoCopyLink(result) {
        const url = `${this.baseUrl}/${encodeURIComponent(result.path)}/${encodeURIComponent(result.filename)}`;
        try {
            await navigator.clipboard.writeText(url);
            this.ui.showToast(this.lang.t('linkCopied'));
        } catch (err) {
            // 剪贴板不可用时静默跳过，用户仍可手动复制
        }
    }

    async uploadAndStore(file) {
        if (this.state.isUploading) {
            this.ui.showToast(this.lang.t('busy'));
            return null;
        }

        this.state.isUploading = true;
        this.state.uploadController = new AbortController();

        try {
            this.ui.showUploadProgress();
            const result = await this.uploadFile(file);
            const fileInfo = {
                path: result.path,
                filename: result.filename,
                fileSize: file.size,
                uploadTime: new Date().toISOString()
            };
            this.storage.saveFileInfo(fileInfo, result.deleteCode);
            return { ...result, fileSize: file.size };
        } catch (error) {
            if (error.name !== 'AbortError') {
                console.error('Upload failed:', error);
                this.ui.showToast(this.lang.t('uploadFailed', { msg: error.message }));
            }
            return null;
        } finally {
            this.state.isUploading = false;
            this.state.uploadController = null;
            this.ui.hideUploadProgress();
        }
    }

    async uploadFile(file) {
        const encodedFilename = encodeURIComponent(file.name);

        return new Promise((resolve, reject) => {
            const xhr = new XMLHttpRequest();

            xhr.upload.addEventListener('progress', (e) => {
                if (e.lengthComputable) {
                    const percent = (e.loaded / e.total) * 100;
                    this.ui.updateProgress(percent, e.loaded, e.total);
                }
            });

            xhr.addEventListener('load', () => {
                if (xhr.status >= 200 && xhr.status < 300) {
                    try {
                        const result = JSON.parse(xhr.responseText);
                        resolve(result);
                    } catch (e) {
                        reject(new Error(this.lang.t('serverFormatError')));
                    }
                } else {
                    reject(new Error(this.lang.t('httpError', { status: xhr.status })));
                }
            });

            xhr.addEventListener('error', () => {
                reject(new Error(this.lang.t('networkError')));
            });

            xhr.addEventListener('abort', () => {
                const err = new Error(this.lang.t('uploadCanceled'));
                err.name = 'AbortError';
                reject(err);
            });

            xhr.open('PUT', `/${encodedFilename}`);
            xhr.setRequestHeader('Accept', 'application/json');

            if (this.state.uploadController) {
                this.state.uploadController.signal.addEventListener('abort', () => {
                    xhr.abort();
                });
            }

            xhr.send(file);
        });
    }

    cancelUpload() {
        if (this.state.uploadController) {
            this.state.uploadController.abort();
            this.ui.showToast(this.lang.t('uploadCanceled'));
        }
    }

    // ---- 文件列表 ----

    async loadFileList() {
        this.dom.fileList.innerHTML = '';
        const files = this.storage.getStoredFiles();
        if (files.length === 0) return;

        const title = document.createElement('h3');
        title.textContent = this.lang.t('filesTitle');
        this.dom.fileList.appendChild(title);

        const fragment = document.createDocumentFragment();
        files.sort((a, b) => new Date(b.uploadTime) - new Date(a.uploadTime))
            .forEach(file => {
                fragment.appendChild(this.ui.createFileListItem(file, (file) => this.handleDelete(file)));
            });

        this.dom.fileList.appendChild(fragment);
    }

    async handleDelete(file) {
        const confirmed = await this.ui.showConfirmDialog(this.lang.t('confirmDelete'));
        if (!confirmed) return;

        try {
            const status = await this.performDelete(file);
            this.storage.removeFileInfo(file);
            this.ui.showToast(status === 200 ? this.lang.t('deleted') : this.lang.t('deletedGone'));
            await this.loadFileList();
        } catch (error) {
            console.error('Delete failed:', error);
            this.ui.showToast(this.lang.t('deleteFailed', { msg: error.message }));
        }
    }

    async performDelete(file) {
        const encodedFilename = encodeURIComponent(file.filename);

        const response = await fetch(
            `/delete/${encodeURIComponent(file.path)}/${encodedFilename}`,
            {
                method: 'DELETE',
                headers: { 'X-Delete-Code': file.deleteCode }
            }
        );

        if (!response.ok && response.status !== 404 && response.status !== 403) {
            throw new Error(`${response.status}`);
        }

        return response.status;
    }

    // ---- 复制 ----

    async copyText(text) {
        try {
            await navigator.clipboard.writeText(text);
            this.ui.showToast(this.lang.t('copied'));
        } catch (err) {
            console.error('Copy failed:', err);
            this.ui.showToast(this.lang.t('copyFailed'));
        }
    }

    async copyResult() {
        const results = this.state.currentResults;
        if (!results || results.length === 0) return;

        const text = results.map(r => {
            const url = `${this.baseUrl}/${encodeURIComponent(r.path)}/${encodeURIComponent(r.filename)}`;
            return `${this.lang.t('fileLink')}: ${url}\n${this.lang.t('deleteCode')}: ${r.deleteCode}`;
        }).join('\n\n');

        await this.copyText(text);
    }
}

// 存储管理器
class StorageManager {
    saveFileInfo(fileInfo, deleteCode) {
        const key = `fileInfo_/${fileInfo.path}/${fileInfo.filename}`;
        try {
            localStorage.setItem(key, JSON.stringify({ ...fileInfo, deleteCode }));
            // 清理旧版分离存储的删除码（如有）
            localStorage.removeItem(`deleteCode_/${fileInfo.path}/${fileInfo.filename}`);
        } catch (error) {
            console.error('Failed to save file info:', error);
        }
    }

    getStoredFiles() {
        const files = [];
        try {
            for (let i = 0; i < localStorage.length; i++) {
                const key = localStorage.key(i);
                if (!key || !key.startsWith('fileInfo_/')) continue;
                try {
                    const data = JSON.parse(localStorage.getItem(key));
                    if (!data || !data.path || !data.filename) continue;
                    // 兼容旧数据：deleteCode 曾经分离存储
                    const deleteCode = data.deleteCode
                        || localStorage.getItem(`deleteCode_/${data.path}/${data.filename}`)
                        || '';
                    files.push({ ...data, deleteCode });
                } catch (error) {
                    console.error('Failed to parse file info:', error);
                    localStorage.removeItem(key);
                }
            }
        } catch (error) {
            console.error('Failed to read stored files:', error);
        }
        return files;
    }

    removeFileInfo(file) {
        try {
            localStorage.removeItem(`deleteCode_/${file.path}/${file.filename}`);
            localStorage.removeItem(`fileInfo_/${file.path}/${file.filename}`);
        } catch (error) {
            console.error('Failed to remove file info:', error);
        }
    }
}

// UI 管理器
class UIManager {
    constructor(dom, lang) {
        this.dom = dom;
        this.lang = lang;
    }

    showUploadProgress() {
        this.dom.uploadProgress.style.display = 'block';
        this.dom.progressBar.style.width = '0%';
        this.dom.statusText.textContent = this.lang.t('preparing');
        this.dom.cancelButton.style.display = 'inline-block';
    }

    hideUploadProgress() {
        this.dom.uploadProgress.style.display = 'none';
        this.dom.cancelButton.style.display = 'none';
    }

    hideUploadResult() {
        this.dom.uploadResult.hidden = true;
    }

    updateProgress(percent, loaded, total) {
        requestAnimationFrame(() => {
            this.dom.progressBar.style.width = percent + '%';
            this.dom.statusText.textContent = this.lang.t('progress', {
                loaded: this.formatFileSize(loaded),
                total: this.formatFileSize(total)
            });
        });
    }

    showUploadResult(results, baseUrl) {
        const items = results.map(r => {
            const encodedPath = encodeURIComponent(r.path);
            const encodedName = encodeURIComponent(r.filename);
            const encodedUrl = `${baseUrl}/${encodedPath}/${encodedName}`;
            const displayUrl = this.escapeHtml(`${baseUrl}/${r.path}/${r.filename}`);
            const filenameLine = results.length > 1
                ? `<p class="result-filename">${this.escapeHtml(r.filename)}</p>`
                : '';
            return `
                <div class="result-item">
                    ${filenameLine}
                    <p>${this.escapeHtml(this.lang.t('fileLink'))}: <a href="${this.escapeHtml(encodedUrl)}" target="_blank" rel="noopener">${displayUrl}</a></p>
                    <p>${this.escapeHtml(this.lang.t('deleteCode'))}: <span class="delete-code">${this.escapeHtml(r.deleteCode)}</span></p>
                </div>
            `;
        }).join('');

        this.dom.resultContent.innerHTML = items;
        this.dom.uploadResult.hidden = false;
    }

    createFileListItem(file, onDelete) {
        const div = document.createElement('div');
        div.className = 'file-item';

        const uploadDate = this.escapeHtml(new Date(file.uploadTime).toLocaleString());
        const downloadUrl = this.escapeHtml(`/${encodeURIComponent(file.path)}/${encodeURIComponent(file.filename)}`);

        div.innerHTML = `
            <div class="file-info">
                <div class="file-name" title="${this.escapeHtml(file.filename)}">${this.escapeHtml(file.filename)}</div>
                <div class="file-meta">
                    <span class="file-size">${this.formatFileSize(file.fileSize)}</span>
                    <span class="upload-time">${uploadDate}</span>
                </div>
            </div>
            <div class="file-actions">
                <a href="${downloadUrl}" class="button" download="${this.escapeHtml(file.filename)}" rel="noopener">${this.escapeHtml(this.lang.t('download'))}</a>
                ${file.deleteCode ? `<button class="button delete-button" type="button">${this.escapeHtml(this.lang.t('delete'))}</button>` : ''}
            </div>
        `;

        if (file.deleteCode) {
            const deleteButton = div.querySelector('.delete-button');
            deleteButton.addEventListener('click', () => onDelete(file));
        }

        return div;
    }

    showConfirmDialog(message) {
        return new Promise((resolve) => {
            const dialog = document.createElement('div');
            dialog.className = 'confirm-dialog';
            dialog.innerHTML = `
                <div class="confirm-dialog-content">
                    <p>${this.escapeHtml(message)}</p>
                    <div class="confirm-dialog-buttons">
                        <button class="confirm-yes" type="button">${this.escapeHtml(this.lang.t('confirmYes'))}</button>
                        <button class="confirm-no" type="button">${this.escapeHtml(this.lang.t('confirmNo'))}</button>
                    </div>
                </div>
            `;

            document.body.appendChild(dialog);
            requestAnimationFrame(() => {
                dialog.style.opacity = '1';
                dialog.classList.add('active');
            });

            const closeDialog = (result) => {
                dialog.style.opacity = '0';
                dialog.classList.remove('active');
                setTimeout(() => {
                    if (document.body.contains(dialog)) {
                        document.body.removeChild(dialog);
                    }
                    resolve(result);
                }, 200);
            };

            dialog.querySelector('.confirm-yes').onclick = () => closeDialog(true);
            dialog.querySelector('.confirm-no').onclick = () => closeDialog(false);

            // ESC 键支持
            const escapeHandler = (e) => {
                if (e.key === 'Escape') {
                    document.removeEventListener('keydown', escapeHandler);
                    closeDialog(false);
                }
            };
            document.addEventListener('keydown', escapeHandler);
        });
    }

    showToast(message, duration = 3000) {
        const toast = document.createElement('div');
        toast.className = 'toast';
        toast.textContent = message;

        document.body.appendChild(toast);
        requestAnimationFrame(() => {
            toast.style.opacity = '1';
        });

        setTimeout(() => {
            toast.style.opacity = '0';
            setTimeout(() => {
                if (document.body.contains(toast)) {
                    document.body.removeChild(toast);
                }
            }, 300);
        }, duration);
    }

    formatFileSize(bytes) {
        if (bytes === 0) return '0 B';
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(1024));
        return parseFloat((bytes / Math.pow(1024, i)).toFixed(2)) + ' ' + sizes[i];
    }

    // 同时转义引号，使其在 HTML 文本与属性（title=、download=、href=）两种上下文下都安全。
    escapeHtml(text) {
        return String(text).replace(/[&<>"']/g, (c) => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;',
        }[c]));
    }
}

// 初始化应用
document.addEventListener('DOMContentLoaded', () => {
    new TinyUpload();
});
