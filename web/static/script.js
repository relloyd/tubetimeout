// ---------- Helper functions for AJAX requests ----------

// Generic function for POST requests (used for saving configuration)
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

// New helper for PUT requests (used for /mode)
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

// ---------- Event Listeners for the Pause and Reset buttons ----------

// Updated Pause button: now uses /mode via PUT.
// (Here we prompt for the group name to pause; you could also tie this to one of your drop‐downs.)
document.getElementById("addButton").addEventListener("click", () => {
    const group = prompt("Enter group to pause:");
    if (group) {
        // Using mode=2 to block traffic. minutes is sent as an integer string.
        putData("/mode", { group: group, minutes: "60", mode: "2" });
    }
});

// Updated Reset button: now requires a group parameter.
document.getElementById("resetButton").addEventListener("click", () => {
    const group = prompt("Enter group to reset usage:");
    if (group) {
        fetch(`/reset?group=${encodeURIComponent(group)}`)
            .then(response => response.text())
            .then(text => { document.getElementById("statusMessage").innerText = text; })
            .catch(error => { document.getElementById("statusMessage").innerText = "Error: " + error.message; });
    }
});

// ---------- Main App Code ----------
document.addEventListener('DOMContentLoaded', () => {
    // Change API URL from /groupMACs to /groups for device-group assignments
    const UrlGroupAPI = '/groups';
    const UrlUsageAPI = '/usage'; // Usage data endpoint
    const trackerConfigAPI = '/trackerConfig'; // Tracker config endpoint
    const saveButton = document.getElementById('save-config-btn'); // Save Configuration Button

    let flatGroupMACs = [];
    // Now groups is an array of objects: { name, retention, startDay, startDuration }
    let groups = [];
    let availableMACs = [];
    let usageData = {}; // Usage data from /usage

    // ---------- Fetch Functions ----------

    // Fetch tracker configuration from /trackerConfig and store it in groups.
    async function fetchTrackerConfig() {
        try {
            const response = await fetch(trackerConfigAPI);
            if (response.ok) {
                const configData = await response.json();
                // Expect configData to be an array of objects
                groups = configData;
            } else {
                console.error("Failed to fetch tracker config.");
            }
        } catch (error) {
            console.error("Error fetching tracker config:", error);
        }
    }

    // Merge any groups found in device assignments into the groups array.
    function mergeDeviceGroups(deviceGroupNames) {
        deviceGroupNames.forEach(name => {
            if (!groups.find(g => g.name === name)) {
                // Add a default tracker config (adjust defaults as needed)
                groups.push({ name, retention: 0, startDay: "0", startDuration: 0 });
            }
        });
    }

    // Fetch the device group assignments and usage data.
    async function fetchConfig() {
        // First, get the tracker configuration
        await fetchTrackerConfig();

        // Now fetch device group assignments
        const response = await fetch(UrlGroupAPI);
        flatGroupMACs = await response.json();

        // Extract group names from devices (ignoring empty strings)
        const deviceGroupNames = [...new Set(flatGroupMACs.map(entry => entry.group).filter(Boolean))];
        mergeDeviceGroups(deviceGroupNames);

        // Fetch usage data
        await fetchUsageData();
        renderDevices();
        renderGroups();
        updateGroupDropdown(); // For device assignment (add-to-group form)
        updateEditGroupDropdown(); // For editing tracker config
    }

    // Fetch usage data from /usage
    async function fetchUsageData() {
        try {
            const response = await fetch(UrlUsageAPI);
            usageData = await response.json();
            console.log("Fetched usage data:", usageData);
        } catch (error) {
            console.error('Error fetching usage data:', error);
            usageData = {};
        }
    }

    // ---------- Render Functions ----------

    // Render Devices Dropdown and Inputs (for assigning a device to a group)
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

    // Update device name input based on selected device
    function updateDeviceNameInput() {
        const mac = document.getElementById('device-dropdown').value;
        const nameInput = document.getElementById('device-name');
        const entry = flatGroupMACs.find(entry => entry.mac === mac);
        nameInput.value = entry && entry.name ? entry.name : '';
    }

    // Update the dropdown used in the Add Group form (for device assignment)
    function updateGroupDropdown() {
        const groupDropdown = document.getElementById('group-dropdown');
        groupDropdown.innerHTML = '';
        // Use the group names from groups array
        groups.forEach(groupObj => {
            const option = document.createElement('option');
            option.value = groupObj.name;
            option.textContent = groupObj.name;
            groupDropdown.appendChild(option);
        });
    }

    // Update the dropdown used in the Edit Group form (for tracker config editing)
    function updateEditGroupDropdown() {
        const dropdown = document.getElementById('edit-group-dropdown');
        dropdown.innerHTML = '';
        groups.forEach(g => {
            const option = document.createElement('option');
            option.value = g.name;
            option.textContent = g.name;
            dropdown.appendChild(option);
        });
        // Populate fields for the first group (if any)
        if (groups.length > 0) {
            dropdown.value = groups[0].name;
            dropdown.dispatchEvent(new Event('change'));
        }
    }

    // Render the groups and their devices, along with tracker config details.
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

            const groupHeader = document.createElement('div');
            groupHeader.classList.add('group-header');

            // Group Title
            const groupTitle = document.createElement('h3');
            groupTitle.textContent = groupName;
            groupHeader.appendChild(groupTitle);

            // Usage Info (if available)
            const usageInfo = document.createElement('span');
            const usage = usageData[groupName.toLowerCase()] || { used: 0, percentage: 0, activity: {} };
            usageInfo.textContent = `${usage.used} mins (${usage.percentage}%) usage`;
            groupHeader.appendChild(usageInfo);

            // Remove Group Button
            const removeGroupBtn = document.createElement('button');
            removeGroupBtn.textContent = 'Remove Group';
            removeGroupBtn.classList.add('remove-group-btn');
            removeGroupBtn.onclick = () => removeGroup(groupName);
            groupHeader.appendChild(removeGroupBtn);

            groupDiv.appendChild(groupHeader);

            // --- Display Tracker Config details (if available) ---
            const groupConfig = groups.find(g => g.name === groupName);
            if (groupConfig) {
                const configInfo = document.createElement('div');
                configInfo.classList.add('group-config-info');
                configInfo.textContent = `Retention: ${groupConfig.retention} day(s), Start Day: ${getDayName(groupConfig.startDay)}, Start Time: ${formatMinutes(groupConfig.startDuration)}`;
                groupDiv.appendChild(configInfo);
            }
            // ------------------------------------------------------

            // List of devices in the group
            const macList = document.createElement('ul');
            grouped[groupName].forEach(({ mac, name }) => {
                const listItem = document.createElement('li');
                const label = document.createElement('span');
                label.textContent = `${mac.replace(/^:/g, '')} - ${name}`;

                // Show last active time if available.
                const lastActiveTimestamp = usage.activity && usage.activity[mac];
                if (lastActiveTimestamp) {
                    label.textContent += ` active ${formatTimeSince(lastActiveTimestamp)}`;
                }

                // Remove device button
                const removeBtn = document.createElement('button');
                removeBtn.textContent = 'Remove';
                removeBtn.onclick = () => removeMacFromGroup(mac);
                listItem.appendChild(label);
                listItem.appendChild(removeBtn);
                macList.appendChild(listItem);
            });

            groupDiv.appendChild(macList);
            groupsContainer.appendChild(groupDiv);
        });
    }

    // ---------- Remove Functions ----------
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
        updateGroupDropdown();
        updateEditGroupDropdown();
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    // ---------- Save Configuration ----------
    // When Save is clicked we POST both the device-group assignments and tracker configuration.
    async function saveConfig() {
        const deviceGroupsToSave = flatGroupMACs.filter(entry => entry.group);
        try {
            // Save device-group assignments (to /groups)
            let response = await fetch(UrlGroupAPI, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(deviceGroupsToSave),
            });
            if (!response.ok) throw new Error('Failed to save device groups');

            // Save tracker configuration (to /trackerConfig)
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

    // ---------- Notification and Save Button Helpers ----------
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

    // ---------- Utility Functions ----------
    function formatTimeSince(timestampString) {
        const timestamp = new Date(timestampString);
        const now = new Date();
        const differenceInSeconds = Math.floor((now - timestamp) / 1000);
        if (differenceInSeconds < 60) {
            return `${differenceInSeconds} second${differenceInSeconds === 1 ? '' : 's'} ago`;
        }
        const differenceInMinutes = Math.floor(differenceInSeconds / 60);
        if (differenceInMinutes < 60) {
            return `${differenceInMinutes} minute${differenceInMinutes === 1 ? '' : 's'} ago`;
        }
        const differenceInHours = Math.floor(differenceInMinutes / 60);
        if (differenceInHours < 24) {
            return `${differenceInHours} hour${differenceInHours === 1 ? '' : 's'} ago`;
        }
        const differenceInDays = Math.floor(differenceInHours / 24);
        return `${differenceInDays} day${differenceInDays === 1 ? '' : 's'} ago`;
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

    // ---------- Event Listeners for Forms ----------

    // Add device to a group
    document.getElementById('add-to-group-btn').onclick = () => {
        const mac = document.getElementById('device-dropdown').value;
        const name = document.getElementById('device-name').value.trim();
        const group = document.getElementById('group-dropdown').value;
        flatGroupMACs.forEach(entry => {
            if (entry.mac === mac) {
                entry.name = name;
                entry.group = group;
            }
        });
        renderDevices();
        renderGroups();
        updateGroupDropdown();
        showSaveButton();
    };

    // Add New Group (with tracker configuration)
    document.getElementById('add-group-form').onsubmit = (e) => {
        e.preventDefault();
        const newGroupName = document.getElementById('new-group-name').value.trim();
        const retention = parseInt(document.getElementById('group-retention').value.trim(), 10);
        const startDay = document.getElementById('group-start-day').value;
        const startTimeStr = document.getElementById('group-start-time').value;
        if (!newGroupName || isNaN(retention) || !startTimeStr) return;

        // Convert HH:MM to minutes past midnight.
        const [hours, minutes] = startTimeStr.split(':').map(Number);
        const startDuration = hours * 60 + minutes;

        if (!groups.find(g => g.name === newGroupName)) {
            groups.push({ name: newGroupName, retention, startDay, startDuration });
            updateGroupDropdown();
            updateEditGroupDropdown();
        }
        // Clear the inputs.
        e.target.reset();
        showSaveButton();
    };

    // Edit Group Form: When a group is selected, populate its fields.
    document.getElementById('edit-group-dropdown').addEventListener('change', () => {
        const selectedGroupName = document.getElementById('edit-group-dropdown').value;
        const group = groups.find(g => g.name === selectedGroupName);
        if (group) {
            document.getElementById('edit-group-retention').value = group.retention;
            document.getElementById('edit-group-start-day').value = group.startDay;
            const hours = Math.floor(group.startDuration / 60).toString().padStart(2, '0');
            const mins = (group.startDuration % 60).toString().padStart(2, '0');
            document.getElementById('edit-group-start-time').value = `${hours}:${mins}`;
        }
    });

    // When Update is clicked in the Edit Group form, update the in‑memory config.
    document.getElementById('update-group-btn').addEventListener('click', () => {
        const selectedGroupName = document.getElementById('edit-group-dropdown').value;
        const retention = parseInt(document.getElementById('edit-group-retention').value, 10);
        const startDay = document.getElementById('edit-group-start-day').value;
        const startTimeStr = document.getElementById('edit-group-start-time').value;
        if (!selectedGroupName || isNaN(retention) || !startTimeStr) return;
        const [hours, minutes] = startTimeStr.split(':').map(Number);
        const startDuration = hours * 60 + minutes;

        const group = groups.find(g => g.name === selectedGroupName);
        if (group) {
            group.retention = retention;
            group.startDay = startDay;
            group.startDuration = startDuration;
            showNotification(`Group ${selectedGroupName} updated.`, false);
            showSaveButton();
            renderGroups();
        }
    });

    saveButton.addEventListener('click', saveConfig);
    fetchConfig();
});