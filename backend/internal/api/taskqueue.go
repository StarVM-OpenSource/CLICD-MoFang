package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

type TaskType string

const (
	TaskCreate    TaskType = "create"
	TaskStart     TaskType = "start"
	TaskStop      TaskType = "stop"
	TaskRestart   TaskType = "restart"
	TaskDelete    TaskType = "delete"
	TaskReinstall TaskType = "reinstall"
)

type Task struct {
	ID            string              `json:"id"`
	Type          TaskType            `json:"type"`
	ContainerID   int                 `json:"container_id"`
	ContainerName string              `json:"container_name"`
	Status        string              `json:"status"`
	Error         string              `json:"error,omitempty"`
	CreatedAt     string              `json:"created_at"`
	TemplateID    string              `json:"template_id,omitempty"`
	Config        lxc.ContainerConfig `json:"config,omitempty"`
	Name          string              `json:"name,omitempty"`
	User          string              `json:"user,omitempty"` // who created this task
}

type TaskQueue struct {
	mu          sync.Mutex
	createQueue []*Task
	opQueue     []*Task
	tasks       map[string]*Task
	nextID      int
	createCond  *sync.Cond
	opCond      *sync.Cond
	stop        chan struct{}
}

var globalQueue *TaskQueue

func init() {
	globalQueue = &TaskQueue{
		tasks: make(map[string]*Task),
		stop:  make(chan struct{}),
	}
	globalQueue.createCond = sync.NewCond(&globalQueue.mu)
	globalQueue.opCond = sync.NewCond(&globalQueue.mu)
	go globalQueue.createWorker()
	go globalQueue.opWorker()
}

func (q *TaskQueue) enqueueTask(task *Task) {
	q.tasks[task.ID] = task
	if task.Type == TaskCreate {
		q.createQueue = append(q.createQueue, task)
		q.createCond.Signal()
	} else {
		q.opQueue = append(q.opQueue, task)
		q.opCond.Signal()
	}
}

func (q *TaskQueue) Enqueue(containerID int, containerName string, taskType TaskType, templateID string, cfg *lxc.ContainerConfig) []string {
	q.mu.Lock()
	defer q.mu.Unlock()

	id := q.nextID
	q.nextID++
	task := &Task{
		ID:            fmt.Sprintf("task-%d", id),
		Type:          taskType,
		ContainerID:   containerID,
		ContainerName: containerName,
		Status:        "pending",
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		TemplateID:    templateID,
	}
	if cfg != nil {
		task.Config = *cfg
	}
	q.enqueueTask(task)
	q.persistTasks()
	return []string{task.ID}
}

func (q *TaskQueue) EnqueueBatch(taskType TaskType, ids []int, templateID string) []string {
	return q.EnqueueBatchWithUser(taskType, ids, templateID, "admin")
}

func (q *TaskQueue) EnqueueBatchWithUser(taskType TaskType, ids []int, templateID string, user string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var result []string
	for _, id := range ids {
		c := config.FindContainer(id)
		name := ""
		if c != nil {
			name = c.Name
		}
		result = append(result, q.enqueueSingleWithUser(id, name, taskType, templateID, user))
	}
	q.persistTasks()
	return result
}

func (q *TaskQueue) EnqueueBatchCreate(configs []lxc.ContainerConfig) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.enqueueBatchCreateList(configs)
}

func (q *TaskQueue) ActiveCreateNames() map[string]bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	names := make(map[string]bool)
	for _, task := range q.tasks {
		if task.Type != TaskCreate || (task.Status != "pending" && task.Status != "running") {
			continue
		}
		name := task.Config.Name
		if name == "" {
			name = task.ContainerName
		}
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func (q *TaskQueue) enqueueBatchCreateList(configs []lxc.ContainerConfig) []string {
	var result []string
	for _, cfg := range configs {
		cfgCopy := cfg
		id := q.nextID
		q.nextID++
		task := &Task{
			ID:            fmt.Sprintf("task-%d", id),
			Type:          TaskCreate,
			ContainerID:   0,
			ContainerName: cfgCopy.Name,
			Status:        "pending",
			CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
			Config:        cfgCopy,
		}
		q.enqueueTask(task)
		result = append(result, task.ID)
	}
	q.persistTasks()
	return result
}

func (q *TaskQueue) enqueueSingle(containerID int, containerName string, taskType TaskType, templateID string) string {
	return q.enqueueSingleWithUser(containerID, containerName, taskType, templateID, "admin")
}

func (q *TaskQueue) enqueueSingleWithUser(containerID int, containerName string, taskType TaskType, templateID string, user string) string {
	id := q.nextID
	q.nextID++
	task := &Task{
		ID:            fmt.Sprintf("task-%d", id),
		Type:          taskType,
		ContainerID:   containerID,
		ContainerName: containerName,
		Status:        "pending",
		CreatedAt:     time.Now().Format("2006-01-02 15:04:05"),
		TemplateID:    templateID,
		User:          user,
	}
	q.enqueueTask(task)
	return task.ID
}

// createWorker handles TaskCreate: lxc-create, resource setup, start, and SSH init.
// If a restored task already has a same-name container in config, it resumes
// initialization instead of creating another ct-{id}.
func (q *TaskQueue) createWorker() {
	for {
		q.mu.Lock()
		for len(q.createQueue) == 0 {
			q.createCond.Wait()
		}
		task := q.createQueue[0]
		q.createQueue = q.createQueue[1:]
		task.Status = "running"
		q.mu.Unlock()

		createdByTask := false
		if task.Config.Name == "" {
			task.Config.Name = task.ContainerName
		}
		if task.Config.Name == "" {
			task.Status = "failed"
			task.Error = "container name is required"
			config.AddAuditLog(string(task.Type), task.ContainerName, "failed: "+task.Error, "admin")
			q.mu.Lock()
			q.persistTasks()
			q.mu.Unlock()
			continue
		}
		c := config.FindContainerByName(task.Config.Name)
		if c == nil {
			// 1) Download image + apply limits (lxc-create)
			err := lxcManager.CreateContainer(task.Config)
			if err != nil {
				task.Status = "failed"
				task.Error = err.Error()
				config.AddAuditLog(string(task.Type), task.Config.Name, "失败: "+err.Error(), "admin")
				q.mu.Lock()
				q.persistTasks()
				q.mu.Unlock()
				continue
			}
			createdByTask = true

			// 2) Find created container by name
			c = config.FindContainerByName(task.Config.Name)
			if c == nil {
				task.Status = "failed"
				task.Error = "created but not found in config"
				config.AddAuditLog(string(task.Type), task.Config.Name, "失败: "+task.Error, "admin")
				q.mu.Lock()
				q.persistTasks()
				q.mu.Unlock()
				continue
			}
		}

		task.ContainerID = c.ID
		task.ContainerName = c.Name

		// 3) Start + initialize SSH/network in the same worker.
		//    If init fails, destroy the container so no dead entry remains.
		startErr := lxcManager.StartContainer(c.ID)
		if startErr != nil {
			if createdByTask {
				lxcManager.DestroyContainer(c.ID)
			}
			task.Status = "failed"
			task.Error = startErr.Error()
			config.AddAuditLog(string(task.Type), task.ContainerName, "初始化失败: "+startErr.Error(), "admin")
		} else {
			task.Status = "done"
			config.AddAuditLog(string(task.Type), task.ContainerName, "成功", "admin")
		}

		q.mu.Lock()
		q.persistTasks()
		q.mu.Unlock()
	}
}

// opWorker handles all non-create tasks (start, stop, restart, delete, reinstall)
// including the follow-up initialization after a create succeeds.
func (q *TaskQueue) opWorker() {
	for {
		q.mu.Lock()
		for len(q.opQueue) == 0 {
			q.opCond.Wait()
		}
		task := q.opQueue[0]
		q.opQueue = q.opQueue[1:]
		task.Status = "running"
		q.mu.Unlock()

		var err error
		err = resolveTaskContainer(task)
		// Block operations on expired or traffic-exceeded containers (except stop/delete)
		if err == nil && (task.Type == TaskStart || task.Type == TaskRestart || task.Type == TaskReinstall) {
			c := config.FindContainer(task.ContainerID)
			if c != nil {
				if lxc.IsExpired(*c) {
					err = fmt.Errorf("容器已到期，不允许此操作")
				} else if lxc.IsTrafficExceeded(*c) {
					err = fmt.Errorf("容器流量已超限，不允许此操作")
				}
			}
		}
		if err == nil {
			switch task.Type {
			case TaskStart:
				err = lxcManager.StartContainer(task.ContainerID)
			case TaskStop:
				err = lxcManager.StopContainer(task.ContainerID)
			case TaskRestart:
				err = lxcManager.RestartContainer(task.ContainerID)
			case TaskDelete:
				err = lxcManager.DestroyContainer(task.ContainerID)
				if err == nil {
					time.Sleep(1 * time.Second)
					if config.FindContainer(task.ContainerID) != nil {
						err = fmt.Errorf("container still exists after delete: %d", task.ContainerID)
					}
				}
			case TaskReinstall:
				err = lxcManager.ReinstallContainer(task.ContainerID, task.TemplateID)
			}
		}

		q.mu.Lock()
		auditUser := task.User
		if auditUser == "" {
			auditUser = "admin"
		}
		if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
			config.AddAuditLog(string(task.Type), task.ContainerName, "失败: "+err.Error(), auditUser)
		} else {
			task.Status = "done"
			config.AddAuditLog(string(task.Type), task.ContainerName, "成功", auditUser)
			switch task.Type {
			case TaskStart:
				config.UpdateContainerStatus(task.ContainerID, "running")
			case TaskStop:
				config.UpdateContainerStatus(task.ContainerID, "stopped")
			case TaskRestart:
				config.UpdateContainerStatus(task.ContainerID, "running")
			}
		}
		q.persistTasks()
		q.mu.Unlock()
	}
}

func resolveTaskContainer(task *Task) error {
	if task.Type == TaskCreate {
		return nil
	}
	if task.ContainerID > 0 {
		if c := config.FindContainer(task.ContainerID); c != nil {
			if task.ContainerName == "" {
				task.ContainerName = c.Name
			}
			return nil
		}
	}
	if task.ContainerName != "" {
		if c := config.FindContainerByName(task.ContainerName); c != nil {
			task.ContainerID = c.ID
			task.ContainerName = c.Name
			return nil
		}
		return fmt.Errorf("container not found: %s", task.ContainerName)
	}
	return fmt.Errorf("container not found: %d", task.ContainerID)
}

func (q *TaskQueue) persistTasks() {
	saved := make([]config.SavedTask, 0)
	for _, t := range q.tasks {
		// Only persist pending and running tasks to avoid
		// re-queuing already completed/failed tasks after restart.
		if t.Status != "pending" && t.Status != "running" {
			continue
		}
		cfgJSON, _ := json.Marshal(t.Config)
		saved = append(saved, config.SavedTask{
			ID:            t.ID,
			Type:          string(t.Type),
			ContainerID:   t.ContainerID,
			ContainerName: t.ContainerName,
			Status:        t.Status,
			Error:         t.Error,
			CreatedAt:     t.CreatedAt,
			TemplateID:    t.TemplateID,
			Config:        string(cfgJSON),
			User:          t.User,
		})
	}
	config.SaveTasks(saved)
}

func (q *TaskQueue) GetTasks() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]*Task, 0, len(q.tasks))
	// Collect all task IDs, sort by creation time (extracted from ID number)
	for _, t := range q.tasks {
		result = append(result, t)
	}
	// Stable sort by ID number (task-N where N is sequential)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if parseIDNum(result[i].ID) > parseIDNum(result[j].ID) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// HandleSingleTaskAction creates a task for a single container action
func HandleSingleTaskAction(w http.ResponseWriter, r *http.Request, id int, action string) {
	c := config.FindContainer(id)
	name := ""
	if c != nil {
		name = c.Name
	}

	// Determine user from JWT claims
	user := "admin"
	if claims, ok := claimsFromRequest(r); ok {
		if subUser, _ := claims["sub_user"].(string); subUser != "" {
			user = "user:" + subUser
		}
	}

	var taskType TaskType
	var templateID string
	switch action {
	case "start":
		taskType = TaskStart
	case "stop":
		taskType = TaskStop
	case "restart":
		taskType = TaskRestart
	case "delete":
		taskType = TaskDelete
	case "reinstall":
		var req struct {
			TemplateID string `json:"template_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		templateID = req.TemplateID
		if templateID == "" {
			c := config.FindContainer(id)
			if c != nil {
				templateID = c.Template
			}
		}
		if !isTemplateEnabledAndDownloaded(templateID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Template is not enabled or downloaded"})
			return
		}
		taskType = TaskReinstall
	default:
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Unknown action"})
		return
	}

	ids := globalQueue.EnqueueBatchWithUser(taskType, []int{id}, templateID, user)
	jsonResponse(w, http.StatusAccepted, APIResponse{
		Success: true,
		Message: "Task queued",
		Data:    map[string]interface{}{"task_id": ids[0], "container_name": name, "status": "pending"},
	})
}

// HandleBatchCreate handles batch container creation
func HandleBatchCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	var req struct {
		Containers []lxc.ContainerConfig `json:"containers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	if len(req.Containers) == 0 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "No containers requested"})
		return
	}

	activeCreateNames := globalQueue.ActiveCreateNames()
	requestNames := make(map[string]bool)
	for i := range req.Containers {
		name := strings.TrimSpace(req.Containers[i].Name)
		req.Containers[i].Name = name
		if !config.IsValidContainerNameSyntax(name) {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid container name: " + name})
			return
		}
		if requestNames[name] {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Duplicate container name in request: " + name})
			return
		}
		if config.FindContainerByName(name) != nil {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Container name already exists: " + name})
			return
		}
		if activeCreateNames[name] {
			jsonResponse(w, http.StatusConflict, APIResponse{Success: false, Message: "Container creation already queued: " + name})
			return
		}
		if req.Containers[i].VCPU <= 0 {
			req.Containers[i].VCPU = 1
		}
		if req.Containers[i].RAMMB < 128 {
			req.Containers[i].RAMMB = 512
		}
		if req.Containers[i].DiskGB < 1 {
			req.Containers[i].DiskGB = 5
		}
		if !isTemplateEnabledAndDownloaded(req.Containers[i].TemplateID) {
			jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: name + ": template is not enabled or downloaded"})
			return
		}
		if req.Containers[i].PortMappingCount < 2 {
			req.Containers[i].PortMappingCount = 2
		}
		if req.Containers[i].SnapshotLimit <= 0 {
			req.Containers[i].SnapshotLimit = config.DefaultSnapshotLimit
		}
		if err := validateContainerResourceRequest(req.Containers[i].VCPU, req.Containers[i].RAMMB, req.Containers[i].DiskGB); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: name + ": " + err.Error()})
			return
		}
		requestNames[name] = true
	}
	ids := globalQueue.EnqueueBatchCreate(req.Containers)
	jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Data: ids})
}

// HandleBatchAction handles batch container actions
func HandleBatchAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	var req struct {
		Action     string `json:"action"`
		Containers []int  `json:"containers"`
		TemplateID string `json:"template_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	var taskType TaskType
	switch req.Action {
	case "start":
		taskType = TaskStart
	case "stop":
		taskType = TaskStop
	case "restart":
		taskType = TaskRestart
	case "delete":
		taskType = TaskDelete
	default:
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Unknown action"})
		return
	}

	ids := globalQueue.EnqueueBatch(taskType, req.Containers, req.TemplateID)
	jsonResponse(w, http.StatusAccepted, APIResponse{Success: true, Data: ids})
}

// HandleTaskDelete deletes a specific task by ID
func HandleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	// URL: /api/tasks/{id}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if taskID == "" {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Task ID required"})
		return
	}
	globalQueue.mu.Lock()
	delete(globalQueue.tasks, taskID)
	// Also remove from both queues if pending
	newCreate := make([]*Task, 0, len(globalQueue.createQueue))
	for _, t := range globalQueue.createQueue {
		if t.ID != taskID {
			newCreate = append(newCreate, t)
		}
	}
	globalQueue.createQueue = newCreate
	newOp := make([]*Task, 0, len(globalQueue.opQueue))
	for _, t := range globalQueue.opQueue {
		if t.ID != taskID {
			newOp = append(newOp, t)
		}
	}
	globalQueue.opQueue = newOp
	globalQueue.persistTasks()
	globalQueue.mu.Unlock()
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Task deleted"})
}

// HandleTasks returns the current task queue
func HandleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	tasks := globalQueue.GetTasks()
	tasks = filterTasksForRequest(r, tasks)
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: tasks})
}

// RestoreTasks restores task queue from config
func RestoreTasks() {
	for _, st := range config.AppConfig.Tasks {
		var cfg lxc.ContainerConfig
		if st.Config != "" {
			json.Unmarshal([]byte(st.Config), &cfg)
		}
		containerName := st.ContainerName
		if containerName == "" {
			containerName = cfg.Name
		}
		if cfg.Name == "" {
			cfg.Name = containerName
		}
		containerID := st.ContainerID
		if containerID <= 0 && containerName != "" {
			if c := config.FindContainerByName(containerName); c != nil {
				containerID = c.ID
			}
		}
		globalQueue.tasks[st.ID] = &Task{
			ID:            st.ID,
			Type:          TaskType(st.Type),
			ContainerID:   containerID,
			ContainerName: containerName,
			Status:        st.Status,
			Error:         st.Error,
			CreatedAt:     st.CreatedAt,
			TemplateID:    st.TemplateID,
			Config:        cfg,
			User:          st.User,
		}
		if st.Status == "pending" || st.Status == "running" {
			// Reset running tasks back to pending so they get retried
			globalQueue.tasks[st.ID].Status = "pending"
			globalQueue.enqueueTask(globalQueue.tasks[st.ID])
		}
		if num := parseIDNum(st.ID); num >= globalQueue.nextID {
			globalQueue.nextID = num + 1
		}
	}
	// Clear persisted tasks from disk (they're now in memory)
	config.SaveTasks([]config.SavedTask{})
}

func parseIDNum(id string) int {
	var num int
	for _, c := range id {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		}
	}
	return num
}
