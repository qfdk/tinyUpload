/**
 * TinyUpload - 文件上传客户端
 * 优化版本：模块化、性能优化、错误处理
 */

class TinyUpload {
    constructor() {
        this.baseUrl = window.location.origin;
        this.state = {
            isUploading: false,
            pendingDeletions: new Set(),
            uploadController: null
        };
        
        this.dom = this.initDOM();
        this.storage = new StorageManager();
        this.ui = new UIManager(this.dom);
        
        this.init();
    }

    initDOM() {
        return {
            dropZone: document.getElementById('dropZone'),
            fileInput: document.getElementById('fileInput'),
            selectButton: document.getElementById('selectButton'),
            uploadProgress: document.getElementById('uploadProgress'),
            progressBar: document.querySelector('.progress-bar-fill'),
            statusText: document.querySelector('.status-text'),
            uploadResult: document.getElementById('uploadResult'),
            resultContent: document.getElementById('resultContent'),
            fileList: document.getElementById('fileList'),
            copyButton: document.getElementById('copyButton')
        };
    }

    init() {
        this.setupEventListeners();
        this.loadFileList();
        this.setupKeyboardShortcuts();
    }

    setupEventListeners() {
        // 文件选择
        this.dom.selectButton.addEventListener('click', () => this.dom.fileInput.click());
        this.dom.fileInput.addEventListener('change', (e) => this.handleFileInputChange(e));
        
        // 复制功能
        this.dom.copyButton.addEventListener('click', () => this.copyResult());
        
        // 拖放
        this.setupDragAndDrop();
    }

    setupKeyboardShortcuts() {
        document.addEventListener('keydown', (e) => {
            if (e.ctrlKey || e.metaKey) {
                switch(e.key) {
                    case 'u':
                        e.preventDefault();
                        this.dom.fileInput.click();
                        break;
                    case 'c':
                        if (this.dom.uploadResult.style.display === 'block') {
                            e.preventDefault();
                            this.copyResult();
                        }
                        break;
                }
            }
            
            if (e.key === 'Escape') {
                this.cancelUpload();
            }
        });
    }

    setupDragAndDrop() {
        let dragCounter = 0;

        this.dom.dropZone.addEventListener('dragenter', (e) => {
            e.preventDefault();
            dragCounter++;
            this.dom.dropZone.classList.add('dragover');
        });

        this.dom.dropZone.addEventListener('dragleave', (e) => {
            e.preventDefault();
            dragCounter--;
            if (dragCounter === 0) {
                this.dom.dropZone.classList.remove('dragover');
            }
        });

        this.dom.dropZone.addEventListener('dragover', (e) => {
            e.preventDefault();
        });

        this.dom.dropZone.addEventListener('drop', async (e) => {
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

    async handleFiles(files) {
        // 验证文件
        const validFiles = this.validateFiles(files);
        if (validFiles.length === 0) return;

        // 单文件上传
        if (validFiles.length === 1) {
            await this.handleFile(validFiles[0]);
        } else {
            // 多文件上传
            await this.handleMultipleFiles(validFiles);
        }
    }

    validateFiles(files) {
        const maxSize = 1024 * 1024 * 1024; // 1GB
        const validFiles = [];

        for (const file of files) {
            if (file.size === 0) {
                this.ui.showToast(`文件 "${file.name}" 为空，已跳过`);
                continue;
            }
            
            if (file.size > maxSize) {
                this.ui.showToast(`文件 "${file.name}" 超过大小限制，已跳过`);
                continue;
            }

            validFiles.push(file);
        }

        return validFiles;
    }

    async handleFile(file) {
        if (this.state.isUploading) {
            this.ui.showToast('正在上传中，请稍候...');
            return;
        }

        this.state.isUploading = true;
        this.state.uploadController = new AbortController();

        try {
            this.ui.showUploadProgress();
            const result = await this.uploadFile(file);
            await this.handleUploadSuccess(result, file);
        } catch (error) {
            if (error.name !== 'AbortError') {
                console.error('上传失败:', error);
                this.ui.showToast('上传失败: ' + error.message);
            }
        } finally {
            this.state.isUploading = false;
            this.state.uploadController = null;
            this.ui.hideUploadProgress();
        }
    }

    async handleMultipleFiles(files) {
        this.ui.showToast(`准备上传 ${files.length} 个文件...`);
        
        for (let i = 0; i < files.length; i++) {
            const file = files[i];
            this.ui.showToast(`上传进度: ${i + 1}/${files.length} - ${file.name}`);
            await this.handleFile(file);
            
            // 给用户一点时间看到结果
            if (i < files.length - 1) {
                await new Promise(resolve => setTimeout(resolve, 1000));
            }
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
                        reject(new Error('服务器响应格式错误'));
                    }
                } else {
                    reject(new Error(`上传失败: ${xhr.status}`));
                }
            });

            xhr.addEventListener('error', () => {
                reject(new Error('网络错误'));
            });

            xhr.addEventListener('abort', () => {
                reject(new Error('上传已取消'));
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
            this.ui.showToast('上传已取消');
        }
    }

    async handleUploadSuccess(result, file) {
        const fileInfo = {
            path: result.path,
            filename: result.filename,
            fileSize: file.size,
            uploadTime: new Date().toISOString()
        };

        this.storage.saveFileInfo(fileInfo, result.deleteCode);
        this.ui.showUploadResult(result, this.baseUrl);
        await this.loadFileList();
    }

    async loadFileList() {
        this.dom.fileList.innerHTML = '';
        const files = this.storage.getStoredFiles();

        if (files.length === 0) return;

        const title = document.createElement('h3');
        title.textContent = '已上传的文件';
        title.style.marginBottom = '15px';
        this.dom.fileList.appendChild(title);

        const fragment = document.createDocumentFragment();
        files.sort((a, b) => new Date(b.uploadTime) - new Date(a.uploadTime))
            .forEach(file => {
                fragment.appendChild(this.ui.createFileListItem(file, (file) => this.handleDelete(file)));
            });

        this.dom.fileList.appendChild(fragment);
    }

    async handleDelete(file) {
        const confirmed = await this.ui.showConfirmDialog('确定要删除这个文件吗？');
        if (!confirmed) return;

        try {
            await this.performDelete(file);
            this.storage.removeFileInfo(file);
            this.ui.showToast('文件已删除');
            await this.loadFileList();
        } catch (error) {
            console.error('删除失败:', error);
            this.ui.showToast('删除失败: ' + error.message);
        }
    }

    async performDelete(file) {
        const encodedFilename = encodeURIComponent(file.filename);
        const encodedDeleteCode = encodeURIComponent(file.deleteCode);

        const response = await fetch(
            `/delete/${file.path}/${encodedFilename}?code=${encodedDeleteCode}`,
            { method: 'DELETE' }
        );

        if (!response.ok) {
            throw new Error(`删除失败: ${response.status}`);
        }
    }

    async copyResult() {
        try {
            const link = this.dom.resultContent.querySelector('a');
            const displayUrl = link.textContent;
            const deleteCode = this.dom.resultContent.textContent.match(/删除码: (.+)/)?.[1] || '';

            const copyText = `文件链接: ${displayUrl}\n删除码: ${deleteCode}`;
            await navigator.clipboard.writeText(copyText);
            this.ui.showToast('复制成功！');
        } catch (err) {
            console.error('复制失败:', err);
            this.ui.showToast('复制失败');
        }
    }
}

// 存储管理器
class StorageManager {
    saveFileInfo(fileInfo, deleteCode) {
        const key = `fileInfo_/${fileInfo.path}/${fileInfo.filename}`;
        try {
            localStorage.setItem(key, JSON.stringify(fileInfo));
            if (deleteCode) {
                localStorage.setItem(
                    `deleteCode_/${fileInfo.path}/${fileInfo.filename}`,
                    deleteCode
                );
            }
        } catch (error) {
            console.error('保存文件信息失败:', error);
        }
    }

    getStoredFiles() {
        const files = [];
        try {
            for (let i = 0; i < localStorage.length; i++) {
                const key = localStorage.key(i);
                if (key && key.startsWith('fileInfo_/')) {
                    try {
                        const fileInfo = JSON.parse(localStorage.getItem(key));
                        const deleteCode = localStorage.getItem(`deleteCode_/${fileInfo.path}/${fileInfo.filename}`);
                        if (fileInfo && fileInfo.path && fileInfo.filename) {
                            files.push({ ...fileInfo, deleteCode });
                        }
                    } catch (error) {
                        console.error('解析文件信息失败:', error);
                        localStorage.removeItem(key);
                    }
                }
            }
        } catch (error) {
            console.error('读取存储文件失败:', error);
        }
        return files;
    }

    removeFileInfo(file) {
        try {
            localStorage.removeItem(`deleteCode_/${file.path}/${file.filename}`);
            localStorage.removeItem(`fileInfo_/${file.path}/${file.filename}`);
        } catch (error) {
            console.error('删除文件信息失败:', error);
        }
    }
}

// UI 管理器
class UIManager {
    constructor(dom) {
        this.dom = dom;
    }

    showUploadProgress() {
        this.dom.uploadProgress.style.display = 'block';
        this.dom.progressBar.style.width = '0%';
        this.dom.uploadResult.style.display = 'none';
        this.dom.statusText.textContent = '准备上传...';
    }

    hideUploadProgress() {
        this.dom.uploadProgress.style.display = 'none';
    }

    updateProgress(percent, loaded, total) {
        requestAnimationFrame(() => {
            this.dom.progressBar.style.width = percent + '%';
            this.dom.statusText.textContent = `已上传 ${this.formatFileSize(loaded)} / ${this.formatFileSize(total)}`;
        });
    }

    showUploadResult(result, baseUrl) {
        this.dom.uploadResult.style.display = 'block';
        const encodedUrl = `${baseUrl}/${result.path}/${encodeURIComponent(result.filename)}`;
        const displayUrl = `${baseUrl}/${result.path}/${this.escapeHtml(result.filename)}`;
        
        this.dom.resultContent.innerHTML = `
            <p>文件链接: <a href="${encodedUrl}" target="_blank" rel="noopener">${displayUrl}</a></p>
            <p>删除码: <span class="delete-code">${this.escapeHtml(result.deleteCode)}</span></p>
        `;
    }

    createFileListItem(file, onDelete) {
        const div = document.createElement('div');
        div.className = 'file-item';

        const uploadDate = new Date(file.uploadTime).toLocaleString();
        const encodedFilename = encodeURIComponent(file.filename);

        div.innerHTML = `
            <div class="file-info">
                <div class="file-name" title="${this.escapeHtml(file.filename)}">${this.escapeHtml(file.filename)}</div>
                <div class="file-meta">
                    <span class="file-size">${this.formatFileSize(file.fileSize)}</span>
                    <span class="upload-time">${uploadDate}</span>
                </div>
            </div>
            <div class="file-actions">
                <a href="/${file.path}/${encodedFilename}" class="button" download="${this.escapeHtml(file.filename)}" rel="noopener">下载</a>
                ${file.deleteCode ? `<button class="button delete-button" type="button">删除</button>` : ''}
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
                        <button class="confirm-yes" type="button">确定</button>
                        <button class="confirm-no" type="button">取消</button>
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

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

// 初始化应用
document.addEventListener('DOMContentLoaded', () => {
    new TinyUpload();
});