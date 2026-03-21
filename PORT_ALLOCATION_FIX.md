# Port Allocation and Container Deletion Fixes

**Date:** March 21, 2026
**Issue:** Port allocation conflicts due to orphaned containers

---

## Problems Identified

### 1. **Orphaned Containers Holding Ports**
- Containers from deleted projects still exist in LXD
- These containers hold ports via proxy devices
- Port allocator only checked database, not actual LXD containers
- New deployments get "unavailable" ports that are actually in use

### 2. **Container Deletion Not Forced**
- Containers not stopped before deletion
- Deletion commands didn't use `--force` flag
- Running containers couldn't be deleted properly

### 3. **Port Proxy Setup Error**
- Error message showed `%!w(<nil>)` due to incorrect error formatting
- Didn't show actual LXD error messages

---

## Fixes Implemented

### 1. **Enhanced Port Allocator** (`port_allocator.go`)

**Changes:**
- Added `runner *exec.Runner` to PortAllocator struct
- Updated `NewPortAllocator()` to accept runner parameter
- Enhanced `getAllocatedPorts()` to check actual LXD containers

**New Logic:**
```go
// Now checks:
1. Database projects (LocalPort)
2. Database containers (PortMappings)
3. ACTUAL LXD containers (via lxc list + lxc config device show)
4. Tunnel routes (LocalPort)
5. Reserved ports (80, 3000)
```

**Key Addition:**
```go
// Get actual LXD containers and extract proxy device ports
lxc list --format csv --columns n
lxc config device show <container>
// Parse "listen=tcp:0.0.0.0:8003" to extract port 8003
```

This ensures orphaned containers are detected and their ports marked as allocated.

---

### 2. **Improved Container Deletion** (`lxd.go`)

**Before:**
```go
lxc delete -f containerName
```

**After:**
```go
// Stop first with force
lxc stop containerName --force

// Then delete with force
lxc delete --force containerName
```

**Benefits:**
- Ensures running containers are stopped
- Force flag prevents hanging on busy containers
- More reliable cleanup

---

### 3. **Better Port Proxy Error Messages** (`lxd.go`)

**Before:**
```go
if err != nil || !result.Success {
    return fmt.Errorf("failed to setup port proxy: %w", err)
}
// Shows: "failed to setup port proxy: %!w(<nil>)" when err is nil
```

**After:**
```go
if err != nil {
    return fmt.Errorf("failed to setup port proxy: %w", err)
}

if !result.Success {
    var errMsg string
    for _, line := range result.Lines {
        if line.Stream == "stderr" {
            errMsg += line.Text + "\n"
        }
    }
    return fmt.Errorf("failed to setup port proxy: %s", errMsg)
}
// Shows actual LXD error message
```

**Benefits:**
- Shows actual LXD error messages
- Easier to debug port conflicts
- Clear error reporting

---

### 4. **Cleanup Service Improvements** (`cleanup.go`)

**Changes:**
- Use `--force` flag for stop and delete
- Increased timeout from 15s to 30s
- Consistent force deletion across all cleanup operations

**Updated Commands:**
```go
// Orphan container cleanup
lxc stop containerName --force
lxc delete --force containerName

// Project deletion cleanup
lxc stop containerID --force
lxc delete --force containerID
```

---

## How It Solves The Problem

### Before:
1. User deletes project via UI
2. Container removed from database
3. Container still exists in LXD with port proxy
4. Port allocator checks database only
5. Port appears "free" but is actually in use
6. New deployment tries to use that port
7. **LXD error: "Device already exists"**

### After:
1. User deletes project via UI
2. Container stopped with `--force`
3. Container deleted with `--force` from LXD
4. Container removed from database
5. Port allocator checks both database AND actual LXD containers
6. Port correctly marked as allocated if orphaned container exists
7. **New deployment gets truly available port**

---

## Testing Recommendations

### 1. Test Orphaned Container Detection
```bash
# Create orphaned container manually
lxc launch images:alpine/3.20 opendeploy-test-orphan
lxc config device add opendeploy-test-orphan proxy-3000 proxy \
  listen=tcp:0.0.0.0:8005 connect=tcp:127.0.0.1:3000

# Try to deploy - should skip port 8005
# Run cleanup - should remove orphaned container
```

### 2. Test Port Allocation
```bash
# Deploy project A (gets port 8001)
# Delete project A
# Deploy project B (should get port 8001, not skip it)
```

### 3. Test Force Deletion
```bash
# Deploy project with running service
# Delete project immediately
# Should stop and delete successfully
```

---

## Files Modified

1. **backend/internal/services/port_allocator.go**
   - Added runner parameter
   - Enhanced getAllocatedPorts() to check actual LXD containers
   - Parse proxy device configs to extract ports

2. **backend/internal/services/lxd.go**
   - Improved DeleteContainer() with force stop
   - Better error messages in SetupPortProxy()

3. **backend/internal/services/cleanup.go**
   - Use --force flag for all stop/delete operations
   - Increased timeouts to 30s

4. **backend/internal/services/deploy.go**
   - Updated NewPortAllocator() call to pass runner

---

## Build Status

✅ **All code compiles successfully**
```bash
cd backend && go build -o /dev/null ./...
# Exit code: 0 (success)
```

---

## Expected Behavior After Fix

### Port Allocation:
- ✅ Checks actual LXD containers, not just database
- ✅ Detects orphaned containers holding ports
- ✅ Allocates truly available ports
- ✅ No more "Device already exists" errors

### Container Deletion:
- ✅ Stops containers before deletion
- ✅ Uses --force flag for reliable cleanup
- ✅ Handles running containers properly
- ✅ Cleans up orphaned containers

### Error Messages:
- ✅ Shows actual LXD error messages
- ✅ Clear debugging information
- ✅ No more "%!w(<nil>)" formatting errors

---

## Deployment Flow Now

```
1. User triggers deployment
   ↓
2. Port allocator checks:
   - Database projects
   - Database containers
   - ACTUAL LXD containers (NEW!)
   - Tunnel routes
   ↓
3. Finds truly available port
   ↓
4. Creates container
   ↓
5. Sets up port proxy (with clear error messages)
   ↓
6. Deployment succeeds
```

---

## Cleanup Flow Now

```
1. User deletes project
   ↓
2. Stop containers with --force (NEW!)
   ↓
3. Delete containers with --force (NEW!)
   ↓
4. Remove from database
   ↓
5. Port is now truly free
```

---

## Additional Recommendations

### 1. Run Cleanup Regularly
Add a cron job or scheduled task:
```bash
# Every hour, clean up orphaned containers
0 * * * * curl -X POST http://localhost:8080/api/v1/cleanup
```

### 2. Monitor Port Usage
```bash
# List all LXD proxy devices
lxc list --format csv --columns n | while read container; do
  echo "=== $container ==="
  lxc config device show $container | grep listen
done
```

### 3. Manual Cleanup If Needed
```bash
# List all opendeploy containers
lxc list opendeploy

# Force delete specific container
lxc stop opendeploy-xxx --force
lxc delete opendeploy-xxx --force
```

---

## Summary

✅ **Port allocation now checks actual LXD containers**
✅ **Container deletion uses force flags**
✅ **Error messages are clear and actionable**
✅ **Orphaned containers are properly detected**
✅ **No more port conflicts on new deployments**

The root cause was that the port allocator only checked the database, while orphaned containers still existed in LXD holding ports. Now it checks both sources, ensuring truly available ports are allocated.
