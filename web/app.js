document.addEventListener('DOMContentLoaded', () => {
    const fileInput = document.getElementById('file-input');
    const dropZone = document.getElementById('drop-zone');
    const fileNameDisplay = document.getElementById('file-name-display');
    const tokenInput = document.getElementById('token');
    const shareForm = document.getElementById('share-form');

    const statusPanel = document.getElementById('status-panel');
    const shareUrlInput = document.getElementById('share-url');
    const copyBtn = document.getElementById('copy-btn');
    const connectionStatus = document.getElementById('connection-status');
    const fileSizeDisplay = document.getElementById('file-size-display');
    const stopBtn = document.getElementById('stop-btn');
    const statusDot = document.querySelector('.status-dot');

	if (location.hash.length > 1) {
		tokenInput.value = location.hash.substring(1);
	}

    let ws = null;
    let selectedFile = null;

    // File selection UI logic
    const updateFileDisplay = (file) => {
        if (file) {
            fileNameDisplay.textContent = file.name;
            fileNameDisplay.classList.add('text-gold');
        } else {
            fileNameDisplay.textContent = 'Click or drag a file to share';
            fileNameDisplay.classList.remove('text-gold');
        }
    };

    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            selectedFile = e.target.files[0];
            updateFileDisplay(selectedFile);
        }
    });

    // Drag and drop logic
    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('dragover');
    });

    dropZone.addEventListener('dragleave', () => {
        dropZone.classList.remove('dragover');
    });

    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('dragover');
        if (e.dataTransfer.files.length > 0) {
            selectedFile = e.dataTransfer.files[0];
            fileInput.files = e.dataTransfer.files; // Sync with file input
            updateFileDisplay(selectedFile);
        }
    });

    // Copy URL logic
    copyBtn.addEventListener('click', () => {
        shareUrlInput.select();
        document.execCommand('copy');

        // Visual feedback
        const originalHTML = copyBtn.innerHTML;
        copyBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="text-gold"><polyline points="20 6 9 17 4 12"></polyline></svg>';
        setTimeout(() => {
            copyBtn.innerHTML = originalHTML;
        }, 2000);
    });

    const formatSize = (bytes) => {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    };

    // WebSocket Transfer Logic
    const startSharing = (token, file) => {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
		const tokenIsFullUrl = token.startsWith('wss://') || token.startsWith('ws://');
        let wsUrl = tokenIsFullUrl ? token : `${protocol}//${host}/ws/${encodeURIComponent(token)}`;

        ws = new WebSocket(wsUrl);

        ws.onopen = () => {
            connectionStatus.textContent = 'Registering...';

            // Send MsgRegister
            const registerMsg = {
                type: 'register',
                file_name: file.name,
                file_size: file.size,
                one_time: false,
                no_cache: false,
                persist: true
            };
            ws.send(JSON.stringify(registerMsg));
        };

        ws.onmessage = async (e) => {
            if (typeof e.data === 'string') {
                const msg = JSON.parse(e.data);

                if (msg.type === 'registered') {
                    // Registration successful
                    shareForm.classList.add('hidden');
                    statusPanel.classList.remove('hidden');

                    connectionStatus.textContent = 'Listening for requests';
                    fileSizeDisplay.textContent = formatSize(file.size);

					const shareLink = 'http' + wsUrl.substring(2).replace(/\/ws\/.*/, `/d/${msg.share_id}/${encodeURIComponent(file.name)}`)
                    //const shareLink = `${window.location.protocol}//${host}/d/${msg.share_id}/${encodeURIComponent(file.name)}`;
                    shareUrlInput.value = shareLink;
                } else if (msg.type === 'file_request') {
                    // Handle file transfer request
                    connectionStatus.textContent = 'Transferring...';
                    await handleFileRequest(msg.request_id, msg.offset, file, ws);
                    connectionStatus.textContent = 'Listening for requests';
                } else if (msg.type === 'error') {
                    connectionStatus.textContent = 'Error: ' + msg.error;
                    statusDot.classList.add('error');
                    statusDot.classList.remove('animate-ping-slow');
                }
            }
        };

        ws.onerror = (err) => {
            console.error('WebSocket Error', err);
            connectionStatus.textContent = 'Connection Error';
            statusDot.classList.add('error');
            statusDot.classList.remove('animate-ping-slow');
        };

        ws.onclose = () => {
            connectionStatus.textContent = 'Disconnected';
            statusDot.classList.add('error');
            statusDot.classList.remove('animate-ping-slow');
            ws = null;
        };
    };

    const handleFileRequest = async (requestId, offset, file, socket) => {
        // Send MsgFileHeader
        const headerMsg = {
            type: 'file_header',
            mime_type: file.type || 'application/octet-stream' // fallback
        };
        socket.send(JSON.stringify(headerMsg));

        // Read and send chunks
        const chunkSize = 1024 * 1024; // 1MB chunks
        let currentOffset = offset || 0;
        let isTransferring = true;

        // Independent recursive progress tracker
        const updateProgress = () => {
            if (!isTransferring || socket.readyState !== WebSocket.OPEN) return;
            // The actual data sent over the wire is what we queued minus what's still in the buffer
            let actualSent = currentOffset - socket.bufferedAmount;
            if (actualSent < 0) actualSent = 0;
            if (actualSent > file.size) actualSent = file.size;

            const progress = Math.round((actualSent / file.size) * 100);
            connectionStatus.textContent = `Transferring... ${progress}%`;
            setTimeout(updateProgress, 100);
        };
        updateProgress();

        while (currentOffset < file.size && socket.readyState === WebSocket.OPEN) {
            // Check if buffer is getting full (> 32MB), if so wait
            if (socket.bufferedAmount > 32 * 1024 * 1024) {
                await new Promise(resolve => setTimeout(resolve, 50));
                continue;
            }

            const end = Math.min(currentOffset + chunkSize, file.size);
            const chunk = file.slice(currentOffset, end);

            // Convert Blob to ArrayBuffer
            const buffer = await chunk.arrayBuffer();

            // Send binary chunk
            if (socket.readyState === WebSocket.OPEN) {
                socket.send(buffer);
            }

            currentOffset = end;
        }

        // Wait for buffer to drain completely before declaring finish
        while (socket.bufferedAmount > 0 && socket.readyState === WebSocket.OPEN) {
            await new Promise(resolve => setTimeout(resolve, 50));
        }

        isTransferring = false;

        // Send MsgFileEnd
        if (socket.readyState === WebSocket.OPEN) {
            connectionStatus.textContent = `Transferring... 100%`;
            const endMsg = { type: 'file_end' };
            socket.send(JSON.stringify(endMsg));
        }
    };

    // Form submission
    shareForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const token = tokenInput.value.trim();
        if (token && selectedFile) {
            startSharing(token, selectedFile);
        }
    });

    // Session termination
    stopBtn.addEventListener('click', () => {
        if (ws) {
            ws.close();
        }
        shareForm.classList.remove('hidden');
        statusPanel.classList.add('hidden');
        statusDot.classList.remove('error');
        statusDot.classList.add('animate-ping-slow');

        // Reset file selection optionally
        // fileInput.value = '';
        // selectedFile = null;
        // updateFileDisplay(null);
    });
});
