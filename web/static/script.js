// ---------- Helper functions for AJAX requests ----------

async function postData(url, data) {
    try {
        const response = await fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: new URLSearchParams(data),
        });
        document.getElementById("statusMessage").innerText = await response.text();
    } catch (error) {
        document.getElementById("statusMessage").innerText = "Error: " + error.message;
    }
}

async function putData(url, data) {
    try {
        const response = await fetch(url, {
            method: "PUT",
            headers: { "Content-Type": "application/x-www-form-urlencoded" },
            body: new URLSearchParams(data),
        });
        document.getElementById("statusMessage").innerText = await response.text();
    } catch (error) {
        document.getElementById("statusMessage").innerText = "Error: " + error.message;
    }
}

// ---------- Global Pause/Resume handlers removed ---
// (Now each group section has its own mode controls)

// ---------- Main App Code ----------
document.addEventListener('DOMContentLoaded', () => {
    // API endpoints – note the use of /groups instead of /groupMACs.
    const UrlGroupAPI = '/groups';
    const UrlUsageAPI = '/usage';
    const trackerConfigAPI = '/trackerConfig';
    const saveButton = document.getElementById('save-config-btn');

    let flatGroupMACs = [];
    // groups will be an array of objects, each with { name, retention, startDay, startDuration }
    let groups = [];
    let availableMACs = [];
    let usageData = {};

    // Fetch tracker configuration – ensure data is an array.
    async function fetchTrackerConfig() {
        try {
            const response = await fetch(trackerConfigAPI);
            if (response.ok) {
                const configData = await response.json();
                groups = Array.isArray(configData) ? configData : Object.values(configData);
            } else {
                console.error("Failed to fetch tracker config.");
            }
        } catch (error) {
            console.error("Error fetching tracker config:", error);
        }
    }

    // Merge any group names found in device assignments into our groups array.
    function mergeDeviceGroups(deviceGroupNames) {
        deviceGroupNames.forEach(name => {
            if (!groups.find(g => g.name === name)) {
                groups.push({ name, retention: 0, startDay: "0", startDuration: 0 });
            }
        });
    }

    // Fetch the device assignments and usage data.
    async function fetchConfig() {
        await fetchTrackerConfig();

        const response = await fetch(UrlGroupAPI);
        flatGroupMACs = await response.json();

        const deviceGroupNames = [...new Set(flatGroupMACs.map(entry => entry.group).filter(Boolean))];
        mergeDeviceGroups(deviceGroupNames);

        await fetchUsageData();
        renderDevices();
        renderGroups();
        updateGroupSelect();
        updateDeviceGroupDropdown();
    }

    async function fetchUsageData() {
        try {
            const response = await fetch(UrlUsageAPI);
            usageData = await response.json();
        } catch (error) {
            console.error('Error fetching usage data:', error);
            usageData = {};
        }
    }

    // Render Devices dropdown (for device assignment)
    function renderDevices() {
        const deviceDropdown = document.getElementById('device-dropdown');
        deviceDropdown.innerHTML = '';
        availableMACs = flatGroupMACs;
        availableMACs.forEach(({ mac, name, group }) => {
            const option = document.createElement('option');
            option.value = mac;
            const label = name ? `${mac} - ${name}` : mac;
            option.textContent = group ? `${label} (in ${group})` : label;
            deviceDropdown.appendChild(option);
        });
        deviceDropdown.onchange = updateDeviceNameInput;
        updateDeviceNameInput();
    }

    function updateDeviceNameInput() {
        const mac = document.getElementById('device-dropdown').value;
        const nameInput = document.getElementById('device-name');
        const entry = flatGroupMACs.find(entry => entry.mac === mac);
        nameInput.value = entry && entry.name ? entry.name : '';
    }

    // Update the dropdown used for device assignment.
    function updateDeviceGroupDropdown() {
        const dropdown = document.getElementById('device-group-dropdown');
        dropdown.innerHTML = '';
        groups.forEach(groupObj => {
            const option = document.createElement('option');
            option.value = groupObj.name;
            option.textContent = groupObj.name;
            dropdown.appendChild(option);
        });
    }

    // Render the groups, their devices, tracker configuration, and mode controls.
    function renderGroups() {
        const groupsContainer = document.getElementById('groups-container');
        groupsContainer.innerHTML = '';

        // Group devices by their assigned groups.
        const grouped = flatGroupMACs.reduce((acc, { group, mac, name }) => {
            if (group) {
                if (!acc[group]) acc[group] = [];
                acc[group].push({ mac, name });
            }
            return acc;
        }, {});

        const sortedGroupNames = Object.keys(grouped).sort();
        sortedGroupNames.forEach(groupName => {
            const groupDiv = document.createElement('div');
            groupDiv.classList.add('group');

            // Group header with title and usage info.
            const groupHeader = document.createElement('div');
            groupHeader.classList.add('group-header');

            const groupTitle = document.createElement('h3');
            groupTitle.textContent = groupName;
            groupHeader.appendChild(groupTitle);

            const usageInfo = document.createElement('span');
            const usage = usageData[groupName.toLowerCase()] || { used: 0, percentage: 0, activity: {} };
            usageInfo.textContent = `${usage.used} mins (${usage.percentage}%) usage`;
            groupHeader.appendChild(usageInfo);

            const removeGroupBtn = document.createElement('button');
            removeGroupBtn.textContent = 'Remove Group';
            removeGroupBtn.onclick = () => removeGroup(groupName);
            groupHeader.appendChild(removeGroupBtn);
            groupDiv.appendChild(groupHeader);

            // Display tracker configuration details.
            const groupConfig = groups.find(g => g.name === groupName);
            if (groupConfig) {
                const configInfo = document.createElement('div');
                configInfo.classList.add('group-config-info');
                configInfo.textContent = `Retention: ${groupConfig.retention} day(s), Start Day: ${getDayName(groupConfig.startDay)}, Start Time: ${formatMinutes(groupConfig.startDuration)}`;
                groupDiv.appendChild(configInfo);
            }

            // List devices in the group.
            const macList = document.createElement('ul');
            grouped[groupName].forEach(({ mac, name }) => {
                const listItem = document.createElement('li');
                const label = document.createElement('span');
                label.textContent = `${mac.replace(/^:/g, '')} - ${name}`;
                const lastActiveTimestamp = usage.activity && usage.activity[mac];
                if (lastActiveTimestamp) {
                    label.textContent += ` active ${formatTimeSince(lastActiveTimestamp)}`;
                }
                const removeBtn = document.createElement('button');
                removeBtn.textContent = 'Remove';
                removeBtn.onclick = () => removeMacFromGroup(mac);
                listItem.appendChild(label);
                listItem.appendChild(removeBtn);
                macList.appendChild(listItem);
            });
            groupDiv.appendChild(macList);

            // ---- Mode Controls for this Group ----
            // Create a small control panel for setting mode (Allow/Block) and duration.
            const modeControls = document.createElement('div');
            modeControls.classList.add('group-mode-controls');

            // Mode select (Allow or Block)
            modeControls.appendChild(document.createTextNode("Mode: "));
            const modeSelect = document.createElement('select');
            modeSelect.classList.add('group-mode-select');
            const optionAllow = document.createElement('option');
            optionAllow.value = "1";
            optionAllow.textContent = "Allow";
            modeSelect.appendChild(optionAllow);
            const optionBlock = document.createElement('option');
            optionBlock.value = "2";
            optionBlock.textContent = "Block";
            modeSelect.appendChild(optionBlock);
            modeControls.appendChild(modeSelect);

            // Duration select
            modeControls.appendChild(document.createTextNode(" Duration: "));
            const durationSelect = document.createElement('select');
            durationSelect.classList.add('group-duration-select');
            const opt15 = document.createElement('option');
            opt15.value = "15";
            opt15.textContent = "15 mins";
            durationSelect.appendChild(opt15);
            const opt60 = document.createElement('option');
            opt60.value = "60";
            opt60.textContent = "1 hour";
            durationSelect.appendChild(opt60);
            const opt120 = document.createElement('option');
            opt120.value = "120";
            opt120.textContent = "2 hours";
            durationSelect.appendChild(opt120);
            const optUntilMidnight = document.createElement('option');
            optUntilMidnight.value = "untilMidnight";
            optUntilMidnight.textContent = "Until Midnight";
            durationSelect.appendChild(optUntilMidnight);
            modeControls.appendChild(durationSelect);

            // "Apply Mode" button sends a PUT to /mode.
            const applyModeButton = document.createElement('button');
            applyModeButton.textContent = "Apply Mode";
            applyModeButton.onclick = () => {
                let duration = durationSelect.value;
                if (duration === "untilMidnight") {
                    const now = new Date();
                    const minutesSinceMidnight = now.getHours() * 60 + now.getMinutes();
                    duration = (24 * 60 - minutesSinceMidnight).toString();
                }
                putData("/mode", { group: groupName, minutes: duration, mode: modeSelect.value });
            };
            modeControls.appendChild(applyModeButton);

            // "Resume" button sends a DELETE to /mode.
            const resumeModeButton = document.createElement('button');
            resumeModeButton.textContent = "Resume";
            resumeModeButton.onclick = () => {
                fetch(`/mode?group=${encodeURIComponent(groupName)}`, { method: 'DELETE' })
                    .then(response => response.text())
                    .then(text => { document.getElementById("statusMessage").innerText = text; })
                    .catch(error => { document.getElementById("statusMessage").innerText = "Error: " + error.message; });
            };
            modeControls.appendChild(resumeModeButton);
            // ---------------------------------------

            groupDiv.appendChild(modeControls);
            groupsContainer.appendChild(groupDiv);
        });
    }

    function removeMacFromGroup(mac) {
        flatGroupMACs.forEach(entry => {
            if (entry.mac === mac) entry.group = '';
        });
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    function removeGroup(groupName) {
        flatGroupMACs.forEach(entry => {
            if (entry.group === groupName) entry.group = '';
        });
        groups = groups.filter(g => g.name !== groupName);
        updateGroupSelect();
        updateDeviceGroupDropdown();
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    // Save both device assignments and tracker configuration.
    async function saveConfig() {
        const deviceGroupsToSave = flatGroupMACs.filter(entry => entry.group);
        try {
            let response = await fetch(UrlGroupAPI, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(deviceGroupsToSave),
            });
            if (!response.ok) throw new Error('Failed to save device groups');

            response = await fetch(trackerConfigAPI, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(groups),
            });
            if (response.ok) {
                showNotification('Configuration saved successfully', false);
            } else {
                showNotification('Failed to save tracker config', true);
            }
        } catch (error) {
            showNotification('Error saving configuration: ' + error.message, true);
        }
        hideSaveButton();
    }

    function showNotification(message, isError = false) {
        const notification = document.getElementById('notification');
        notification.textContent = message;
        notification.classList.toggle('error', isError);
        notification.classList.remove('hidden');
        setTimeout(() => {
            notification.classList.add('hidden');
        }, 3000);
    }

    function showSaveButton() {
        saveButton.style.display = 'inline-block';
    }

    function hideSaveButton() {
        saveButton.style.display = 'none';
    }

    function formatTimeSince(timestampString) {
        const timestamp = new Date(timestampString);
        const now = new Date();
        const diffSeconds = Math.floor((now - timestamp) / 1000);
        if (diffSeconds < 60) return `${diffSeconds} second${diffSeconds === 1 ? '' : 's'} ago`;
        const diffMinutes = Math.floor(diffSeconds / 60);
        if (diffMinutes < 60) return `${diffMinutes} minute${diffMinutes === 1 ? '' : 's'} ago`;
        const diffHours = Math.floor(diffMinutes / 60);
        if (diffHours < 24) return `${diffHours} hour${diffHours === 1 ? '' : 's'} ago`;
        const diffDays = Math.floor(diffHours / 24);
        return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`;
    }

    function getDayName(day) {
        const days = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
        return days[parseInt(day, 10)] || "Unknown";
    }

    function formatMinutes(totalMinutes) {
        const hours = Math.floor(totalMinutes / 60);
        const minutes = totalMinutes % 60;
        return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}`;
    }

    // ---------- Consolidated Group Configuration Form Handlers ----------
    function updateGroupSelect() {
        const groupSelect = document.getElementById('group-select');
        groupSelect.innerHTML = '';
        const newOption = document.createElement('option');
        newOption.value = "";
        newOption.textContent = "-- New Group --";
        groupSelect.appendChild(newOption);
        groups.forEach(groupObj => {
            const option = document.createElement('option');
            option.value = groupObj.name;
            option.textContent = groupObj.name;
            groupSelect.appendChild(option);
        });
        groupSelect.dispatchEvent(new Event('change'));
    }

    document.getElementById('group-select').addEventListener('change', (e) => {
        const selectedName = e.target.value;
        const nameInput = document.getElementById('group-name');
        const retentionInput = document.getElementById('group-retention');
        const startDaySelect = document.getElementById('group-start-day');
        const startTimeInput = document.getElementById('group-start-time');
        if (selectedName === "") {
            nameInput.value = "";
            nameInput.disabled = false;
            retentionInput.value = "";
            startDaySelect.value = "0";
            startTimeInput.value = "";
        } else {
            const group = groups.find(g => g.name === selectedName);
            if (group) {
                nameInput.value = group.name;
                nameInput.disabled = true;
                retentionInput.value = group.retention;
                startDaySelect.value = group.startDay;
                const hours = Math.floor(group.startDuration / 60).toString().padStart(2, '0');
                const mins = (group.startDuration % 60).toString().padStart(2, '0');
                startTimeInput.value = `${hours}:${mins}`;
            }
        }
    });

    document.getElementById('save-group-btn').addEventListener('click', () => {
        const groupSelect = document.getElementById('group-select');
        const selectedName = groupSelect.value;
        const nameInput = document.getElementById('group-name').value.trim();
        const retention = parseInt(document.getElementById('group-retention').value, 10);
        const startDay = document.getElementById('group-start-day').value;
        const startTimeStr = document.getElementById('group-start-time').value;
        if (!nameInput || isNaN(retention) || !startTimeStr) {
            alert("Please fill in all fields.");
            return;
        }
        const [hours, minutes] = startTimeStr.split(':').map(Number);
        const startDuration = hours * 60 + minutes;
        if (selectedName === "") {
            if (!groups.find(g => g.name === nameInput)) {
                groups.push({ name: nameInput, retention, startDay, startDuration });
                updateGroupSelect();
                updateDeviceGroupDropdown();
            } else {
                alert("Group already exists!");
            }
        } else {
            const group = groups.find(g => g.name === selectedName);
            if (group) {
                group.retention = retention;
                group.startDay = startDay;
                group.startDuration = startDuration;
                showNotification(`Group ${group.name} updated.`, false);
            }
        }
        showSaveButton();
        renderGroups();
    });

    // Device assignment: add a device to a group.
    document.getElementById('add-to-group-btn').onclick = () => {
        const mac = document.getElementById('device-dropdown').value;
        const name = document.getElementById('device-name').value.trim();
        const group = document.getElementById('device-group-dropdown').value;
        flatGroupMACs.forEach(entry => {
            if (entry.mac === mac) {
                entry.name = name;
                entry.group = group;
            }
        });
        renderDevices();
        renderGroups();
        showSaveButton();
    };

    saveButton.addEventListener('click', saveConfig);
    fetchConfig();
});