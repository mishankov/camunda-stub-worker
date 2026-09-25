<script lang="ts">
  import { onMount } from 'svelte'
  import { Boxes, Cable, Check, ChevronDown, ChevronLeft, ChevronRight, Clock3, Download, Ellipsis, History, Pencil, Plus, Play, Square, Waypoints, X } from '@lucide/svelte'
  import { call, on } from './lib/api'
  import JsonEditor from './lib/JsonEditor.svelte'
  import Modal from './lib/Modal.svelte'
  import ActionMenu from './lib/ActionMenu.svelte'

  type Profile = { operateAuthMode:OperateAuth['mode']; operateUsername?:string; operatePassword?:string; operateToken?:string; operateUrl:string; id:string; name:string; host:string; port:number; selectedVersion:string; compatibilityVerified:boolean; commandTimeoutMs:number; activationTimeoutMs:number; renewalIntervalMs:number; maxActiveJobs:number; historyRetentionDays:number }
  type JobType = { id:string; profileId:string; jobType:string; description:string; mode:'manual'|'auto'; activeScenarioId:string|null; maxActiveJobs:number }
  type Scenario = { id:string; jobTypeConfigId:string; name:string; description:string; outcome:'success'|'business_error'|'technical_failure'; variablesJson:string; delayMs:number; errorCode:string; errorMessage:string; remainingRetries:number; retryBackoffMs:number }
  type RuntimeState = { configId:string; state:string; activeJobs:number; lastError:string }
  type Activation = { id:string; jobTypeConfigId:string; jobKey:string; jobType:string; mode:'manual'|'auto'; processInstanceKey:string; bpmnProcessId:string; processDefinitionKey:string; elementId:string; retries:number; inputJson:string; customHeadersJson:string; scenarioSnapshotJson:string; draftJson:string; sendStatus:string; activationState:string; activationStateReason:string; receivedAt:string }
  type Draft = { outcome:'success'|'business_error'|'technical_failure'; variablesJson:string; errorCode:string; errorMessage:string; remainingRetries:number; retryBackoffMs:number }
  type Confirmation = { title:string; message:string; target?:string; confirmLabel:string; action:()=>Promise<void> }
  type UpdateInfo = { currentVersion:string; latestVersion:string; updateAvailable:boolean; releaseUrl:string }

  let tab:'types'|'process'|'pending'|'history'|'settings' = 'process'
  type ProcessInstance = { bpmnProcessId:string; version:number; processDefinitionKey:string; processInstanceKey:string }
  type DeployedProcess = { bpmnProcessId:string; name:string; latestVersion:number; versions:{version:number;key:string}[] }
  let deployedProcesses:DeployedProcess[] = [], loadingProcesses = false, processesLoaded = false, processesError = '', processSourceKey = '', processListRequest = 0
  type ProcessTask = {id:string;name:string;jobType:string;dynamic:boolean}
  let processTasks:ProcessTask[] = [], tasksLoading = false, tasksError = '', tasksSource = '', tasksRequest = 0
  $: selectedProcess = deployedProcesses.find(p=>p.bpmnProcessId===processId)
  $: selectedDefinition = selectedProcess?.versions?.find(v=>v.version===(processVersion??selectedProcess.latestVersion))
  $: taskSource = selectedDefinition ? `${processSourceKey}|${processId}|${selectedDefinition.key}` : ''
  $: if(taskSource!==tasksSource) void loadTasks(taskSource, selectedDefinition?.key||'', processId)
  $: processPending = pending.filter(a=>a.bpmnProcessId===processId && (!selectedDefinition || a.processDefinitionKey===selectedDefinition.key))
  $: processWorkers = jobTypes.filter(t=>processTasks.some(task=>!task.dynamic&&task.jobType===t.jobType))
  async function loadTasks(source:string,key:string,id:string){
    tasksSource=source;const request=++tasksRequest;processTasks=[];tasksError='';tasksLoading=!!source
    if(!source)return
    try {const result=await call<ProcessTask[]>('GetProcessTasks',selectedProfileId,key,id);if(request===tasksRequest)processTasks=result||[]}
    catch(e:any){if(request===tasksRequest)tasksError=e?.message||String(e)}
    finally {if(request===tasksRequest)tasksLoading=false}
  }
  async function addTaskResponse(task:ProcessTask){
    await run(async()=>{
      let type=jobTypes.find(t=>t.jobType===task.jobType)
      if(!type){type=await call<JobType>('SaveJobType',{id:'',profileId:selectedProfileId,jobType:task.jobType,description:'',mode:'manual',activeScenarioId:null,maxActiveJobs:1});await reload()}
      newScenario(type.id)
    })
  }
  async function stopProcessWorkers(){
    const workers=processWorkers.filter(type=>rt(type.id).state==='running'||rt(type.id).state==='stopping')
    await runWorkerCommand(workers.map(type=>type.id),async()=>{for(const type of workers)await call('StopType',type.id)})
  }
  async function startProcessWorkers(){
    const workers=processWorkers.filter(type=>rt(type.id).state!=='running')
    await runWorkerCommand(workers.map(type=>type.id),async()=>{for(const type of workers)await call('StartType',type.id)})
  }
  type OperateAuth = { mode:'none'|'token'|'password'; token:string; username:string; password:string }
  const emptyOperateAuth = ():OperateAuth => ({mode:'none',token:'',username:'',password:''})
  let editingOperateAuth = emptyOperateAuth()
  let processId = '', processVersion:number|undefined, processVariables = '{}'
  let showProcessStart = false
  let startingProcess = false, processError = '', processResult:ProcessInstance|null = null, processResultProfile = ''
  let appVersion = ''
  let loading = true, busy = false, error = ''
  let errorTimer:number|undefined
  let profiles:Profile[] = [], selectedProfileId = '', jobTypes:JobType[] = [], scenarios:Scenario[] = [], runtime:RuntimeState[] = [], pending:Activation[] = []
  let connection:any = { state:'offline', message:'Connection not checked', selectedVersion:'8.5' }
  let dataPath = ''
  let refreshingLiveState = false
  let historyRefreshTimer:number|undefined
  let refreshingHistory = false
  let historyRefreshQueued = false
  let currentProfile:Profile|undefined
  let updateInfo:UpdateInfo|null = null, checkingForUpdates = false
  let editingType:JobType|null = null, editingScenario:Scenario|null = null, editingProfile:Profile|null = null
  let modalOperation:{owner:object;label:string;error:string}|null = null
  async function runModal(owner:object,label:string,action:()=>Promise<void>){
    if(busy)return
    busy=true;modalOperation={owner,label,error:''}
    try {await action()}
    catch(e:any){modalOperation={owner,label:'',error:e?.message||String(e)}}
    finally {busy=false;if(modalOperation?.owner===owner)modalOperation={...modalOperation,label:''}}
  }
  let attemptsLoading=false, attemptsError='', attemptsRequest=0
  let confirmation:Confirmation|null = null
  let drafts:Record<string,Draft> = {}
  let draftScenarios:Record<string,string> = {}
  let history:any = { items:[], page:1, pageSize:100, total:0 }, historySearch = '', historyStatus = '', historyOutcome = '', historyType = '', historyFrom = '', historyTo = '', historyPage = 1, selectedHistory:Activation|null = null, attempts:any[] = []
  $: currentProfile = profiles.find(p=>p.id===selectedProfileId)
  $: profileDeletionReasons = Object.fromEntries(profiles.map(profile=>[profile.id,profileDeletionReason(profile,profiles.length,selectedProfileId,runtime,pending.length)]))
  $: if (currentProfile && processSourceKey !== `${currentProfile.id}|${currentProfile.operateUrl||''}`) resetProcessSource(`${currentProfile.id}|${currentProfile.operateUrl||''}`)
  $: if (tab==='process' && currentProfile?.operateUrl && !processesLoaded && !loadingProcesses) void loadProcesses()
  $: currentViewLabel = ({types:'Job types',process:'Processes',pending:'Awaiting response',history:'History',settings:'Connections'} as const)[tab]

  const outcomeLabel = (v:string) => ({success:'Success',business_error:'Business error',technical_failure:'Technical failure'} as any)[v] || v
  const sendLabel = (v:string) => ({not_prepared:'Not prepared',prepared:'Response prepared',sending:'Sending',confirmed:'Confirmed by Camunda',failed:'Send failed',unknown:'Outcome unknown'} as any)[v] || v
  const stateLabel = (v:string) => ({stopped:'Stopped',running:'Running',stopping:'Stopping',error:'Error'} as any)[v] || v
  const rt = (id:string):RuntimeState => runtime.find(x=>x.configId===id) || {configId:id,state:'stopped',activeJobs:0,lastError:''}
  $: allWorkersRunning = jobTypes.length>0 && jobTypes.every(type=>runtime.some(worker=>worker.configId===type.id&&worker.state==='running'))
  $: hasActiveProcessWorkers = processWorkers.some(type=>runtime.some(worker=>worker.configId===type.id&&(worker.state==='running'||worker.state==='stopping')))
  $: allProcessWorkersRunning = processWorkers.length>0 && processWorkers.every(type=>runtime.some(worker=>worker.configId===type.id&&worker.state==='running'))
  const scenarioName = (id:string|null) => scenarios.find(x=>x.id===id)?.name || 'Not selected'
  const fmtDate = (v:string) => v ? new Date(v).toLocaleString('en-US') : '—'
  const parseDraft = (a:Activation):Draft => { try { return JSON.parse(a.draftJson) } catch { return {outcome:'success',variablesJson:'{}',errorCode:'',errorMessage:'',remainingRetries:0,retryBackoffMs:0} } }

  const notificationDurationMs = 5000
  function showError(message:string) { error=message; if(errorTimer)window.clearTimeout(errorTimer);errorTimer=message?window.setTimeout(()=>{error='';errorTimer=undefined},notificationDurationMs):undefined }

  async function run(fn:()=>Promise<any>) { showError(''); busy=true; try { await fn() } catch(e:any) { showError(e?.message || String(e)) } finally { busy=false } }
  async function reload() { const b = await call<any>('Bootstrap'); appVersion=b.appVersion||''; profiles=b.profiles||[]; selectedProfileId=b.selectedProfileId; jobTypes=b.jobTypes||[]; scenarios=b.scenarios||[]; runtime=b.runtime||[]; connection=b.connection; pending=b.pending||[]; dataPath=b.dataPath; for(const a of pending) if(!drafts[a.id]) { drafts[a.id]=parseDraft(a); try { draftScenarios[a.id]=JSON.parse(a.scenarioSnapshotJson).id||'' } catch { draftScenarios[a.id]='' } } loading=false }
  async function refreshLiveState() { if(refreshingLiveState)return;refreshingLiveState=true;try{const b=await call<any>('Bootstrap');runtime=b.runtime||[];connection=b.connection;pending=b.pending||[];for(const a of pending)if(!drafts[a.id]){drafts[a.id]=parseDraft(a);try{draftScenarios[a.id]=JSON.parse(a.scenarioSnapshotJson).id||''}catch{draftScenarios[a.id]=''}}}catch{}finally{refreshingLiveState=false} }
  async function selectProfile(event:Event) {
    const select=event.currentTarget as HTMLSelectElement
    if(workerCommandIds.length){select.value=selectedProfileId;showError('Wait for the worker operation to finish before switching connections.');return}
    const previous=selectedProfileId
    selectedProfileId=select.value
    await run(async()=>{try{await call('SelectProfile',selectedProfileId);await reload()}catch(e){selectedProfileId=previous;throw e}})
  }
  async function checkConnection() { await run(async()=>{ connection=await call('CheckConnection') }) }
  function resetProcessSource(key:string){
    tasksRequest++;tasksSource='';processTasks=[];tasksError='';tasksLoading=false;processResult=null;processError='';processSourceKey=key;processListRequest++;deployedProcesses=[];processesLoaded=false;loadingProcesses=false;processesError='';processId='';processVersion=undefined
  }
  async function loadProcesses(){
    const profileId=selectedProfileId, request=++processListRequest
    loadingProcesses=true;processesError='';processesLoaded=true
    try {
      const result=await call<DeployedProcess[]>('ListProcesses',profileId)
      if(request!==processListRequest)return
      deployedProcesses=result||[]
      if(!deployedProcesses.some(p=>p.bpmnProcessId===processId))processId=''
      if(processVersion&&!deployedProcesses.find(p=>p.bpmnProcessId===processId)?.versions?.some(v=>v.version===processVersion))processVersion=undefined
    } catch(e:any) {if(request===processListRequest){deployedProcesses=[];processesError=e?.message||String(e)}}
    finally {if(request===processListRequest)loadingProcesses=false}
  }
  async function startProcess(){
    if(startingProcess||busy||!selectedDefinition)return
    startingProcess=true;processError='';processResult=null
    processResultProfile=currentProfile?.name||selectedProfileId
    try {
      processResult=await call<ProcessInstance>('StartProcess',{profileId:selectedProfileId,bpmnProcessId:processId,version:selectedDefinition?.version??processVersion??0,variablesJson:processVariables})
    } catch(e:any) { processError=e?.message||String(e) } finally { startingProcess=false }
  }
  async function formatProcessVariables(){if(busy||startingProcess)return;busy=true;processError='';const original=processVariables;try{const formatted=await call<string>('FormatJSON',original);if(processVariables===original)processVariables=formatted}catch(e:any){processError=e?.message||String(e)}finally{busy=false}}
  let workerCommandIds:string[] = []
  async function runWorkerCommand(ids:string[],action:()=>Promise<void>){
    if(busy||ids.some(id=>workerCommandIds.includes(id)||savingWorkerIds.includes(id)))return
    workerCommandIds=[...workerCommandIds,...ids];showError('')
    try{await action()}catch(e:any){showError(e?.message||String(e))}
    finally{workerCommandIds=workerCommandIds.filter(id=>!ids.includes(id))}
  }
  async function startAll(){await runWorkerCommand(jobTypes.map(type=>type.id),()=>call('StartAll'))}
  async function stopAll(){await runWorkerCommand(jobTypes.map(type=>type.id),()=>call('StopAll'))}
  async function startType(id:string){await runWorkerCommand([id],()=>call('StartType',id))}
  async function stopType(id:string){await runWorkerCommand([id],()=>call('StopType',id))}

  function newType(){ editingType={id:'',profileId:selectedProfileId,jobType:'',description:'',mode:'manual',activeScenarioId:null,maxActiveJobs:1} }
  async function saveType(){const owner=editingType;if(!owner)return;await runModal(owner,'Saving…',async()=>{await call('SaveJobType',{...owner});await reload();if(editingType===owner)editingType=null})}
  function requestDeleteType(type:JobType){confirmation={title:'Delete job type?',message:'Its response scenarios will also be deleted. Activation history will be preserved.',target:type.jobType,confirmLabel:'Delete job type',action:async()=>{await call('DeleteJobType',type.id);if(editingType===type)editingType=null;await reload()}}}
  function newScenario(typeId:string){editingScenario={id:'',jobTypeConfigId:typeId,name:'',description:'',outcome:'success',variablesJson:'{}',delayMs:0,errorCode:'',errorMessage:'',remainingRetries:0,retryBackoffMs:0}}
  async function saveScenario(){const owner=editingScenario;if(!owner)return;await runModal(owner,'Saving…',async()=>{await call('SaveScenario',{...owner});await reload();if(editingScenario===owner)editingScenario=null})}
  async function duplicateScenario(id:string){const owner=editingScenario;if(!owner)return;await runModal(owner,'Duplicating…',async()=>{const copy=await call<Scenario>('DuplicateScenario',id);await reload();if(editingScenario===owner){editingScenario={...copy};requestAnimationFrame(()=>document.querySelector<HTMLInputElement>('dialog [data-modal-initial-focus]')?.focus())}})}
  function requestDeleteScenario(scenario:Scenario){confirmation={title:'Delete response?',message:'This response scenario will be removed from its job type.',target:scenario.name,confirmLabel:'Delete response',action:async()=>{await call('DeleteScenario',scenario.id);if(editingScenario===scenario)editingScenario=null;await reload()}}}
  let expandedPendingTasks:Record<string,boolean> = {}
  let savingWorkerIds:string[] = []
  async function saveWorkerSelection(type:JobType, value:string, field:'mode'|'activeScenarioId'){
    if(savingWorkerIds.includes(type.id))return
    savingWorkerIds=[...savingWorkerIds,type.id]
    showError('')
    try {
      if(field==='mode'&&value==='auto'&&!type.activeScenarioId)throw new Error('Select an active response before switching to Automatic mode.')
      const saved=await call<JobType>('SaveJobType',{...type,[field]:value||null})
      jobTypes=jobTypes.map(t=>t.id===saved.id?saved:t)
    } catch(e:any) {
      showError(e?.message||String(e))
    } finally {
      savingWorkerIds=savingWorkerIds.filter(id=>id!==type.id)
    }
  }
  async function formatScenario(){const owner=editingScenario;if(!owner)return;const original=owner.variablesJson;await runModal(owner,'Formatting…',async()=>{const formatted=await call<string>('FormatJSON',original);if(editingScenario===owner&&owner.variablesJson===original)editingScenario.variablesJson=formatted})}

  let selectedPending:Record<string,string> = {}
  let pendingOperations:Record<string,string> = {}, pendingErrors:Record<string,string> = {}
  let savedDrafts:Record<string,string> = {}
  function draftStatus(a:Activation){
    if(pendingOperations[a.id])return pendingOperations[a.id]
    const saved=savedDrafts[a.id]??JSON.stringify(parseDraft(a))
    return JSON.stringify(drafts[a.id])===saved?'Draft saved':'Unsaved changes'
  }
  async function pendingAction(a:Activation,label:string,action:()=>Promise<void>){
    if(pendingOperations[a.id])return
    pendingOperations={...pendingOperations,[a.id]:label};pendingErrors={...pendingErrors,[a.id]:''}
    try{await action()}catch(e:any){pendingErrors={...pendingErrors,[a.id]:e?.message||String(e)}}
    finally{pendingOperations={...pendingOperations,[a.id]:''}}
  }
  async function submit(a:Activation){const d={...drafts[a.id]};await pendingAction(a,'Sending…',async()=>{await call('SaveDraft',a.id,d);savedDrafts={...savedDrafts,[a.id]:JSON.stringify(d)};await call('SubmitResponse',a.id,d);await reload()})}
  async function saveDraft(a:Activation){const d={...drafts[a.id]};await pendingAction(a,'Saving…',async()=>{await call('SaveDraft',a.id,d);savedDrafts={...savedDrafts,[a.id]:JSON.stringify(d)}})}
  async function formatDraft(a:Activation){if(draftScenarios[a.id])return;const original=drafts[a.id].variablesJson;await pendingAction(a,'Formatting…',async()=>{const formatted=await call<string>('FormatJSON',original);if(drafts[a.id]?.variablesJson===original)drafts[a.id].variablesJson=formatted})}
  function draftFromScenario(s:Scenario):Draft {
    return {outcome:s.outcome,variablesJson:s.variablesJson,errorCode:s.errorCode,errorMessage:s.errorMessage,remainingRetries:s.remainingRetries,retryBackoffMs:s.retryBackoffMs}
  }
  async function applyDraftScenario(a:Activation,id:string){
    if(pendingOperations[a.id])return
    if(!id){draftScenarios[a.id]='';return}
    const scenario=scenarios.find(s=>s.id===id&&s.jobTypeConfigId===a.jobTypeConfigId)
    if(!scenario)return
    const previous={...drafts[a.id]}, previousId=draftScenarios[a.id]
    const replacement=draftFromScenario(scenario), snapshot=JSON.stringify(replacement)
    drafts[a.id]=replacement;draftScenarios[a.id]=id
    await pendingAction(a,'Applying response…',async()=>{
      try{
        const saved=await call<Draft>('ApplyScenario',a.id,id)
        savedDrafts={...savedDrafts,[a.id]:JSON.stringify(saved)}
        if(JSON.stringify(drafts[a.id])===snapshot)drafts[a.id]=saved
      }catch(e){
        if(JSON.stringify(drafts[a.id])===snapshot){drafts[a.id]=previous;draftScenarios[a.id]=previousId}
        else draftScenarios[a.id]=''
        throw e
      }
    })
  }

  async function loadHistory(page=1){historyPage=page;history=await call('History',{profileId:selectedProfileId,jobType:historyType,outcome:historyOutcome,sendStatus:historyStatus,search:historySearch,fromUtc:historyFrom?new Date(historyFrom+'T00:00:00').toISOString():'',toUtc:historyTo?new Date(historyTo+'T23:59:59.999').toISOString():'',page})}
  function queueHistoryRefresh(){
    if(tab!=='history')return
    if(historyRefreshTimer!==undefined)window.clearTimeout(historyRefreshTimer)
    historyRefreshTimer=window.setTimeout(()=>{historyRefreshTimer=undefined;void refreshHistoryFromEvent()},100)
  }
  async function refreshHistoryFromEvent(){
    if(tab!=='history')return
    if(refreshingHistory){historyRefreshQueued=true;return}
    refreshingHistory=true
    try {
      do {
        historyRefreshQueued=false
        await loadHistory(historyPage)
      } while(historyRefreshQueued&&tab==='history')
    } catch(e:any) {
      error=e?.message||String(e)
    } finally {
      refreshingHistory=false
    }
  }
  async function openHistory(a:Activation){
    const request=++attemptsRequest;selectedHistory=a;attempts=[];attemptsLoading=true;attemptsError=''
    try {const result=await call<any[]>('Attempts',a.id);if(request===attemptsRequest&&selectedHistory===a)attempts=result||[]}
    catch(e:any){if(request===attemptsRequest&&selectedHistory===a)attemptsError=e?.message||String(e)}
    finally {if(request===attemptsRequest)attemptsLoading=false}
  }
  function requestClearHistory(){confirmation={title:'Clear completed history?',message:'Completed activation records for this profile will be deleted. Active records will be preserved.',confirmLabel:'Clear history',action:async()=>{await call('ClearHistory',selectedProfileId,true);await loadHistory(1)}}}
  async function confirmAction(){const pending=confirmation;if(!pending)return;await runModal(pending,pending.confirmLabel.startsWith('Delete')?'Deleting…':'Clearing…',async()=>{await pending.action();if(confirmation===pending)confirmation=null})}

  function authForProfile(profile:Profile|undefined):OperateAuth {return {mode:profile?.operateAuthMode||'none',username:profile?.operateUsername||'',password:profile?.operatePassword||'',token:profile?.operateToken||''}}
  function closeProfile(){editingProfile=null;editingOperateAuth=emptyOperateAuth()}
  function editCurrentProfile(){editingProfile={...profiles.find(p=>p.id===selectedProfileId)!};editingOperateAuth=authForProfile(editingProfile)}
  function newProfile(){editingOperateAuth={mode:'password',username:'demo',password:'demo',token:''};editingProfile={operateAuthMode:'password',operateUrl:'http://localhost:8081',id:'',name:'New profile',host:'localhost',port:26500,selectedVersion:'8.5',compatibilityVerified:true,commandTimeoutMs:10000,activationTimeoutMs:120000,renewalIntervalMs:30000,maxActiveJobs:10,historyRetentionDays:30}}
  function profileDeletionReason(profile:Profile,count:number,selectedId:string,workers:RuntimeState[],pendingCount:number):string {
    if(count<=1)return 'At least one connection profile must remain.'
    if(profile.id===selectedId)return 'Select another profile using the Profile menu before deleting this one.'
    if(workers.some(worker=>worker.state==='running'||worker.state==='stopping'||worker.activeJobs>0)||pendingCount)return 'Stop workers and finish active jobs before deleting a profile.'
    return ''
  }
  function requestDeleteProfile(profile:Profile){
    if(busy||startingProcess||profileDeletionReasons[profile.id])return
    confirmation={title:'Delete connection profile?',message:'Its saved credentials, job types, and response scenarios will also be deleted. Activation history will remain in the local database, but will no longer be accessible through this profile. This cannot be undone.',target:profile.name,confirmLabel:'Delete profile',action:async()=>{await call('DeleteProfile',profile.id);await reload()}}
  }
  async function saveProfile(){const owner=editingProfile;if(!owner)return;await runModal(owner,'Saving…',async()=>{const auth={...editingOperateAuth};const saved=await call<Profile>('SaveProfile',{...owner,operateAuthMode:auth.mode,operateUsername:auth.username,operatePassword:auth.password,operateToken:auth.token});if(!selectedProfileId){selectedProfileId=saved.id;await call('SelectProfile',saved.id)};await reload();if(saved.id===selectedProfileId){processListRequest++;deployedProcesses=[];processesError='';processesLoaded=false;loadingProcesses=false;}if(editingProfile===owner)closeProfile()})}
  async function openDataDirectory(){await run(async()=>{await call('OpenDataDirectory')})}
  async function exportProfile(){await run(async()=>{await call('ExportProfileFile',selectedProfileId)})}
  async function importProfile(){await run(async()=>{await call('ImportProfileFile');await reload()})}
  async function checkForUpdates(manual=false){
    if(checkingForUpdates)return
    checkingForUpdates=true
    try {
      updateInfo=await call<UpdateInfo>('CheckForUpdates')
    } catch(e:any) {
      if(manual)showError(e?.message||String(e))
    } finally {
      checkingForUpdates=false
    }
  }
  async function openReleasesPage(){await call('OpenReleasesPage')}

  onMount(()=>{const offs=[on('runtime:changed',(v)=>runtime=v),on('connection:changed',(v)=>connection=v),on('activation:created',(a)=>{if(a.mode==='manual'){pending=[...pending,a];drafts[a.id]=parseDraft(a)}queueHistoryRefresh()}),on('activation:changed',(a)=>{const keep=a.mode==='manual'&&a.activationState==='active';pending=keep?(pending.some(x=>x.id===a.id)?pending.map(x=>x.id===a.id?a:x):[...pending,a]):pending.filter(x=>x.id!==a.id);queueHistoryRefresh()}),on('storage:error',(v)=>showError('SQLite: '+v))];const liveTimer=window.setInterval(()=>void refreshLiveState(),1500);void reload().then(()=>checkForUpdates()).catch((e:any)=>{showError(e?.message||String(e));loading=false});return()=>{window.clearInterval(liveTimer);if(historyRefreshTimer!==undefined)window.clearTimeout(historyRefreshTimer);if(errorTimer)window.clearTimeout(errorTimer);offs.forEach(f=>f())}})
</script>

{#snippet workerCard(task:ProcessTask, type:JobType|undefined, waiting:Activation[], cardKey:string, description='')}
            {@const worker = runtime.find(state=>state.configId===type?.id) || {state:'stopped',lastError:''}}
            <article class="process-task">
              <div class="process-task-head">
                <div class="process-task-identity"><div class="process-task-title"><h3>{task.name||task.id}</h3>{#if type&&!task.dynamic}<span class="status {worker.state}">{stateLabel(worker.state)}</span>{/if}</div>{#if task.jobType!==(task.name||task.id)}<code>{task.jobType}</code>{/if}</div>
                {#if type&&!task.dynamic}<div class="process-task-header-controls"><div class="mode-switch" role="group" aria-label={`Mode for ${task.name||task.id}`}>{#each [{value:'manual',label:'Manual'},{value:'auto',label:'Automatic'}] as mode}<button type="button" aria-pressed={type.mode===mode.value} disabled={busy||savingWorkerIds.includes(type.id)} on:click={()=>{if(type.mode!==mode.value)void saveWorkerSelection(type,mode.value,'mode')}}>{mode.label}</button>{/each}</div><button class="icon-btn" aria-label={`Worker settings for ${task.name||task.id}`} title="Worker settings" disabled={busy||savingWorkerIds.includes(type.id)} on:click={()=>editingType={...type}}><Ellipsis size={18}/></button></div>{/if}
              </div>
              {#if description}<p class="hint">{description}</p>{/if}
              {#if task.dynamic}<p class="hint">This job type is an expression resolved at runtime. Configure its resolved value in Job types.</p>
              {:else if type}

                {#if worker.lastError}<div class="inline-error">{worker.lastError}</div>{/if}
                <div class="worker-responses" role="group" aria-label={`Responses for ${task.name||task.id}`}><span class="responses-label">Responses</span>{#each scenarios.filter(s=>s.jobTypeConfigId===type.id) as response}<div class="response-choice" class:chosen={response.id===type.activeScenarioId}><button class="response-select" aria-pressed={response.id===type.activeScenarioId} title={`${response.name} · ${outcomeLabel(response.outcome)}`} aria-label={`Select ${response.name} · ${outcomeLabel(response.outcome)}`} disabled={busy||savingWorkerIds.includes(type.id)} on:click={()=>{if(type.activeScenarioId!==response.id)void saveWorkerSelection(type,response.id,'activeScenarioId')}}><span>{response.name}</span></button><button class="response-edit" aria-label={`Edit ${response.name}`} title="Edit response" disabled={busy||savingWorkerIds.includes(type.id)} on:click={()=>editingScenario={...response}}><Pencil size={13}/></button></div>{/each}<button class="add-response" disabled={busy} on:click={()=>newScenario(type.id)}><Plus size={12}/> Add response</button></div>
              {:else}<div class="worker-responses"><span class="hint">No responses configured</span><button class="add-response" disabled={busy} on:click={()=>addTaskResponse(task)}><Plus size={12}/> Add response</button></div>{/if}
              <div class="process-task-footer">
                {#if waiting.length}<button class="pending-attention with-icon" aria-expanded={!!expandedPendingTasks[cardKey]} aria-controls={`pending-task-${cardKey}`} on:click={()=>expandedPendingTasks={...expandedPendingTasks,[cardKey]:!expandedPendingTasks[cardKey]}}><span class="pending-count">{waiting.length}</span><span>{waiting.length===1?'job':'jobs'} awaiting response</span><ChevronDown size={16} class="pending-chevron"/></button>{:else}<span class="hint">No jobs awaiting response</span>{/if}
                {#if type&&!task.dynamic}{#if worker.state==='running'||worker.state==='stopping'}<button class="worker-stop with-icon" disabled={busy||workerCommandIds.includes(type.id)} on:click={()=>stopType(type.id)}><Square size={12}/> Stop worker</button>{:else}<button class="worker-start with-icon" disabled={busy||savingWorkerIds.includes(type.id)||workerCommandIds.includes(type.id)} on:click={()=>startType(type.id)}><Play size={12}/> Start worker</button>{/if}{/if}
              </div>
              {#if waiting.length}<div class="task-pending" id={`pending-task-${cardKey}`} hidden={!expandedPendingTasks[cardKey]}>{@render pendingQueue(waiting,cardKey,true)}</div>{/if}
            </article>
{/snippet}

{#snippet pendingQueue(items:Activation[],queueKey:string,embedded=false)}
  {@const selected=items.find(a=>a.id===selectedPending[queueKey])||items[0]}
  {#if selected}
  <section class="pending-workspace" class:embedded aria-label="Pending job queue">
    <div class="pending-queue" role="group" aria-label="Choose a pending job">
      <div class="pending-queue-count">{#if embedded}Pending jobs{:else}{items.length} {items.length===1?'job':'jobs'} awaiting response{/if}</div>
      {#each items as a (a.id)}
        <button type="button" class="pending-queue-item" aria-pressed={a.id===selected.id} aria-label={`Job ${a.jobKey}`} on:click={()=>selectedPending={...selectedPending,[queueKey]:a.id}}>
          <strong>{embedded?'Job':a.jobType} · …{a.jobKey.slice(-4)}</strong>
          <span>{#if !embedded}{a.bpmnProcessId} · {/if}Instance …{a.processInstanceKey.slice(-4)}</span>
          <small>{pendingErrors[a.id]?'Response needs attention':draftStatus(a)==='Draft saved'?'Ready':draftStatus(a)}</small>
        </button>
      {/each}
    </div>
    <div class="pending-editor">
      {#key selected.id}{#if drafts[selected.id]}{@render pendingEditor(selected)}{/if}{/key}
    </div>
  </section>
  {/if}
{/snippet}

{#snippet pendingEditor(a:Activation)}
  <div class="pending-editor-heading"><div><h3>{a.jobType} · Job …{a.jobKey.slice(-4)}</h3><p>{a.bpmnProcessId} · instance …{a.processInstanceKey.slice(-4)}</p></div><span class="status running">Activation valid</span></div>
  <div class="pending-context"><span class="field-label">Input variables</span><pre>{a.inputJson}</pre><span class="field-label">Custom headers</span><pre>{a.customHeadersJson}</pre></div>
  <fieldset class="pending-response-fields" disabled={!!pendingOperations[a.id]||busy||a.sendStatus==='sending'}>
    <legend>Response for this job</legend>
    <div class="pending-response-choices" role="group" aria-label="Saved responses">
      {#each scenarios.filter(s=>s.jobTypeConfigId===a.jobTypeConfigId) as scenario}
        <button type="button" aria-pressed={draftScenarios[a.id]===scenario.id} on:click={()=>applyDraftScenario(a,scenario.id)}>{scenario.name}</button>
      {/each}
      <button type="button" aria-pressed={!draftScenarios[a.id]} on:click={()=>applyDraftScenario(a,'')}>Custom</button>
    </div>
    <label>Outcome<select bind:value={drafts[a.id].outcome}><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select></label>
    {#if drafts[a.id].outcome==='business_error'}<div class="two"><label>Error code<input bind:value={drafts[a.id].errorCode}/></label><label>Error message<input bind:value={drafts[a.id].errorMessage}/></label></div>
    {:else if drafts[a.id].outcome==='technical_failure'}<label>Failure message<input bind:value={drafts[a.id].errorMessage}/></label><div class="two"><label>Retries remaining<input type="number" min="0" bind:value={drafts[a.id].remainingRetries}/></label><label>Retry delay, ms<input type="number" min="0" bind:value={drafts[a.id].retryBackoffMs}/></label></div><small class="hint">This is the retry count sent to Camunda, not a decrement. Reusing a positive value may cause repeated activations.</small>{/if}
    <div class="editor-field-head"><span class="field-label">Response variables</span><button type="button" class="link" disabled={!!draftScenarios[a.id]} on:click={()=>formatDraft(a)}>Format JSON</button></div>
  </fieldset>
  <JsonEditor bind:value={drafts[a.id].variablesJson} ariaLabel={`Response variables for job ${a.jobKey}`} readOnly={!!draftScenarios[a.id]||!!pendingOperations[a.id]||busy||a.sendStatus==='sending'} compact/>
  <details class="pending-context"><summary>Job details</summary><dl><dt>Job key</dt><dd><code>{a.jobKey}</code></dd><dt>Process instance</dt><dd><code>{a.processInstanceKey}</code></dd><dt>BPMN task</dt><dd><code>{a.elementId}</code></dd><dt>Received</dt><dd>{fmtDate(a.receivedAt)}</dd></dl></details>
  {#if pendingErrors[a.id]}<div class="inline-error" role="alert">{pendingErrors[a.id]}</div>{/if}
  <div class="pending-editor-footer"><span class="hint" role="status">{draftStatus(a)}</span><button class="ghost" on:click={()=>saveDraft(a)} disabled={busy||!!pendingOperations[a.id]||a.sendStatus==='sending'}>{pendingOperations[a.id]==='Saving…'?'Saving…':'Save draft'}</button><button class="primary" on:click={()=>submit(a)} disabled={busy||!!pendingOperations[a.id]||a.sendStatus==='sending'}>{pendingOperations[a.id]==='Sending…'||a.sendStatus==='sending'?'Sending…':'Send to Camunda'}</button></div>
{/snippet}

<svelte:head><title>Camunda Stub Worker</title></svelte:head>

{#if loading}<div class="splash"><span class="spinner"></span>Opening local database…</div>{:else}
<div class="app-shell">
  <aside>
    <div class="brand"><Waypoints size={17}/><strong>Stub Worker</strong></div>
    <nav>
      <button class:active={tab==='process'} on:click={()=>tab='process'}><span><Waypoints size={18}/></span> Processes</button>
      <button class:active={tab==='types'} on:click={()=>tab='types'}><span><Boxes size={18}/></span> Job types</button>
      <button class:active={tab==='pending'} on:click={()=>tab='pending'}><span><Clock3 size={18}/></span> Awaiting response <b>{pending.length}</b></button>
      <button class:active={tab==='history'} on:click={()=>{tab='history';loadHistory(1)}}><span><History size={18}/></span> History</button>
      <button class:active={tab==='settings'} on:click={()=>tab='settings'}><span><Cable size={18}/></span> Connections</button>
    </nav>
    {#if appVersion}<div class="sidebar-version" aria-label="App version">{appVersion}</div>{/if}
  </aside>
  <main>
    <header>
      <strong class="view-title">{currentViewLabel}</strong>
      <div class="profile-select"><label for="profile-select">Profile</label><select id="profile-select" value={selectedProfileId} on:change={selectProfile} disabled={busy||startingProcess}>{#each profiles as p}<option value={p.id}>{p.name}</option>{/each}</select></div>
      <div class="connection"><span class:ok={connection.state==='connected'} class:warn={connection.state==='version_mismatch'}></span><div><strong>{connection.state==='connected'?'Connected':connection.state==='version_mismatch'?'Versions differ':'Not connected'}</strong><small>{connection.detectedVersion ? `Server ${connection.detectedVersion} · selected ${connection.selectedVersion}` : `${profiles.find(p=>p.id===selectedProfileId)?.host}:${profiles.find(p=>p.id===selectedProfileId)?.port}`}</small></div></div>
      <button class="ghost" on:click={checkConnection} disabled={busy}>Check connection</button>
    </header>

    {#if updateInfo?.updateAvailable}<div class="update-banner" role="status"><Download size={18}/><div><strong>Camunda Stub Worker {updateInfo.latestVersion} is available</strong><small>You are using {updateInfo.currentVersion}. Download the new release when convenient.</small></div><button class="secondary" on:click={openReleasesPage}>View release</button><button class="icon-btn" aria-label="Dismiss update notification" on:click={()=>updateInfo=null}><X size={17}/></button></div>{/if}

    {#if error}<div class="toast-region" aria-live="polite">
      {#if error}<div class="toast error" role="alert"><span>{error}</span><button aria-label="Dismiss error" on:click={()=>showError('')}><X size={16}/></button></div>{/if}
    </div>{/if}

    {#if tab==='types'}
      <section class="workspace">
        <div class="command-bar"><strong>{jobTypes.length} {jobTypes.length===1?'job type':'job types'}</strong><div class="actions"><button class="secondary with-icon" on:click={newType}><Plus size={14}/> Add</button>{#if jobTypes.length>1}{#if jobTypes.some(type=>runtime.some(worker=>worker.configId===type.id&&(worker.state==='running'||worker.state==='stopping')))}<button class="worker-stop with-icon" on:click={stopAll} disabled={busy||workerCommandIds.length>0}><Square size={12}/> Stop all</button>{/if}{#if !allWorkersRunning}<button class="worker-start with-icon" on:click={startAll} disabled={busy||workerCommandIds.length>0}><Play size={12}/> Start all</button>{/if}{/if}</div></div>
        {#if !jobTypes.length}<div class="empty"><div><Boxes size={24} strokeWidth={1.5}/></div><h3>No job types</h3><p>Add the exact job type from your BPMN model, then define its responses.</p><button class="secondary with-icon" on:click={newType}><Plus size={14}/> Add job type</button></div>{/if}
        {#if jobTypes.length}<div class="process-tasks">
          {#each jobTypes as type (type.id)}
            {@render workerCard({id:type.id,name:type.jobType,jobType:type.jobType,dynamic:false},type,pending.filter(a=>a.jobTypeConfigId===type.id),`type-${type.id}`,type.description)}
          {/each}
        </div>{/if}
      </section>
    {:else if tab==='process'}
      <section class="workspace">
        <div class="process-form">
          <div class="process-overview-head"><div><h2>Choose a process</h2><p class="hint">Configure shared responses, handle waiting jobs, and start a process instance.</p></div></div>
          {#if startingProcess}<p class="hint" role="status">Starting process…</p>{/if}
          {#if processError&&!showProcessStart}<div class="inline-error" role="alert">Process could not be started: {processError}</div>{/if}
          <div class="two">
            <label>BPMN process ID
                <select bind:value={processId} required disabled={startingProcess||loadingProcesses||!currentProfile?.operateUrl} on:change={()=>processVersion=undefined}><option value="">{loadingProcesses?'Loading processes…':'Select a deployed process'}</option>{#each deployedProcesses as process}<option value={process.bpmnProcessId}>{process.name ? `${process.name} — ` : ''}{process.bpmnProcessId} (v{process.latestVersion})</option>{/each}</select>
            </label>
            <label>Version
            {#if selectedProcess}<select aria-label="Process version" bind:value={processVersion} disabled={startingProcess}><option value={undefined}>Latest (v{selectedProcess.latestVersion})</option>{#each selectedProcess.versions||[] as version}<option value={version.version}>Version {version.version}</option>{/each}</select>
            {:else}<select aria-label="Process version" disabled><option>Select a process first</option></select>{/if}</label>
          </div>
          <div class="process-selection-actions">
            {#if currentProfile?.operateUrl}
            <div class="actions"><button type="button" class="ghost" on:click={loadProcesses} disabled={loadingProcesses||startingProcess}>{loadingProcesses?'Loading…':'Refresh processes'}</button></div>
            {/if}
            <button type="button" class="worker-start with-icon" disabled={!selectedDefinition} on:click={()=>{if(!startingProcess){processResult=null;processError=''}showProcessStart=true}}><Play size={14}/> Start process…</button>
          </div>
          {#if currentProfile?.operateUrl}
            <p class="hint">Uses the Operate authentication configured in <button type="button" class="link" on:click={editCurrentProfile}>connection settings</button>.</p>
            {#if processesError}<div class="inline-error" role="alert">{processesError} Check connection settings and refresh the process list.</div>{:else if processesLoaded&&!loadingProcesses&&!deployedProcesses.length}<p class="hint">No deployed processes found in the default tenant. Recently deployed models may take a moment to appear in Operate.</p>{/if}
          {:else}<p class="hint">To choose from deployed processes, <button type="button" class="link" on:click={editCurrentProfile}>set the Operate URL in your connection profile</button>.</p>{/if}
        </div>
        {#if processId}
          <div class="command-bar process-task-heading"><div><strong>Worker tasks</strong><p class="hint">Responses, mode, and worker controls are shared by job type across this connection.</p></div><div class="actions">{#if hasActiveProcessWorkers}<button class="worker-stop with-icon" disabled={busy||tasksLoading||processWorkers.some(type=>workerCommandIds.includes(type.id))} on:click={stopProcessWorkers}><Square size={12}/> Stop all</button>{/if}{#if !allProcessWorkersRunning}<button class="worker-start with-icon" disabled={busy||tasksLoading||!processWorkers.length||processWorkers.some(type=>workerCommandIds.includes(type.id))} on:click={startProcessWorkers}><Play size={12}/> Start all</button>{/if}</div></div>
          {#if tasksLoading}<p class="hint" role="status">Loading BPMN tasks…</p>
          {:else if tasksError}<div class="inline-error" role="alert">{tasksError} <button class="link" on:click={()=>loadTasks(taskSource,selectedDefinition?.key||'',processId)}>Retry</button></div>
          {:else if !selectedDefinition}<p class="hint">Select a deployed process version to discover its tasks.</p>
          {:else if !processTasks.length}<div class="table-empty">This process version has no worker tasks. Tasks inside called processes are configured separately.</div>
          {:else}
          <div class="process-tasks">
          {#each processTasks as task (task.id)}
            {@const type = jobTypes.find(t=>t.jobType===task.jobType)}
            {@const waiting = processPending.filter(a=>a.elementId===task.id)}
            {@render workerCard(task,type,waiting,`process-${selectedDefinition?.key}-${task.id}`)}
          {/each}
          </div>
          {/if}
          <p class="hint">Pending jobs are manual jobs activated by this app for the selected process version. Workers can also receive jobs from other processes using the same job type.</p>
          {@render pendingQueue(processPending.filter(a=>!processTasks.some(task=>task.id===a.elementId)),'process-unmatched')}
        {:else}<div class="empty"><div><Waypoints size={24}/></div><h3>Select a BPMN process to get started</h3><p>Its worker tasks and saved responses will appear here.</p></div>{/if}
      </section>
    {:else if tab==='pending'}
      <section class="workspace">
      {#if !pending.length}<div class="empty"><div><Check size={24} strokeWidth={1.7}/></div><h3>No jobs awaiting response</h3><p>Jobs activated by manual workers will appear here.</p></div>{/if}
      {@render pendingQueue(pending,'all-pending')}
      </section>
    {:else if tab==='history'}
      <section class="workspace"><div class="command-bar"><strong>{history.total||0} records</strong><button class="danger-text" on:click={requestClearHistory}>Clear completed</button></div>
      <div class="filters"><div class="history-filter-main"><input aria-label="Job key or process instance key" bind:value={historySearch} placeholder="Job key or process instance key" on:keydown={(e)=>e.key==='Enter'&&loadHistory(1)}/><select aria-label="Job type filter" bind:value={historyType}><option value="">All job types</option>{#each jobTypes as t}<option value={t.jobType}>{t.jobType}</option>{/each}</select><select aria-label="Outcome filter" bind:value={historyOutcome}><option value="">All outcomes</option><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select><select aria-label="Status filter" bind:value={historyStatus}><option value="">All statuses</option><option value="confirmed">Confirmed</option><option value="failed">Failed</option><option value="unknown">Unknown</option><option value="prepared">Prepared</option></select></div><div class="history-filter-dates"><label>From<input aria-label="From date" type="date" bind:value={historyFrom}/></label><label>To<input aria-label="To date" type="date" bind:value={historyTo}/></label><button class="secondary" on:click={()=>loadHistory(1)}>Search</button></div></div>
      <div class="table-wrap"><table><thead><tr><th>Received</th><th>Job type</th><th>Job key</th><th>Process</th><th>Mode</th><th>Send status</th></tr></thead><tbody>{#each history.items as a}<tr on:click={()=>openHistory(a)}><td>{fmtDate(a.receivedAt)}</td><td><span class="pill">{a.jobType}</span></td><td><code>{a.jobKey}</code></td><td><code>{a.processInstanceKey}</code></td><td>{a.mode==='auto'?'Automatic':'Manual'}</td><td><span class="send {a.sendStatus}">{sendLabel(a.sendStatus)}</span></td></tr>{/each}</tbody></table>{#if !history.items?.length}<div class="table-empty">No records match these filters</div>{/if}</div>
      <div class="pagination"><span>Total: {history.total||0}</span><button aria-label="Previous page" disabled={historyPage<=1} on:click={()=>loadHistory(historyPage-1)}><ChevronLeft size={15}/></button><b>{historyPage}</b><button aria-label="Next page" disabled={historyPage*100>=history.total} on:click={()=>loadHistory(historyPage+1)}><ChevronRight size={15}/></button></div>
      </section>
    {:else}
      <section class="workspace"><div class="command-bar"><strong>{profiles.length} {profiles.length===1?'profile':'profiles'}</strong><button class="secondary with-icon" on:click={newProfile}><Plus size={14}/> New profile</button></div>
      <div class="saved-profiles" aria-label="Saved connection profiles">
        {#each profiles as profile (profile.id)}
          <div class="saved-profile">
            <div><strong>{profile.name}{profile.id===selectedProfileId?' (current)':''}</strong><code>{profile.host}:{profile.port}</code>{#if profileDeletionReasons[profile.id]}<small>{profileDeletionReasons[profile.id]}</small>{/if}</div>
            <button class="danger-text" aria-label={`Delete connection profile ${profile.name}`} disabled={busy||startingProcess||!!profileDeletionReasons[profile.id]} on:click={()=>requestDeleteProfile(profile)}>Delete</button>
          </div>
        {/each}
      </div>
      <div class="settings-grid"><article><h2>Current profile</h2>{#if currentProfile}<dl><dt>Address</dt><dd><code>{currentProfile.host}:{currentProfile.port}</code></dd><dt>Operate URL</dt><dd>{currentProfile.operateUrl||'Not configured'}</dd><dt>Operate authentication</dt><dd>{({none:'None',password:'Username and password',token:'Bearer token'} as const)[currentProfile.operateAuthMode||'none']}</dd><dt>API version</dt><dd>{currentProfile.selectedVersion} {#if !currentProfile.compatibilityVerified}<span class="warn-text">Compatibility not verified</span>{/if}</dd><dt>Command timeout</dt><dd>{currentProfile.commandTimeoutMs} ms</dd><dt>Activation timeout</dt><dd>{currentProfile.activationTimeoutMs} ms</dd><dt>Renewal</dt><dd>every {currentProfile.renewalIntervalMs} ms</dd><dt>Global limit</dt><dd>{currentProfile.maxActiveJobs}</dd><dt>History</dt><dd>{currentProfile.historyRetentionDays} days</dd></dl><button class="primary" on:click={editCurrentProfile}>Edit profile</button>{/if}</article><article><h2>Local data</h2><p>SQLite is stored in the standard user data directory. History is not included in exports.</p><code class="path">{dataPath}</code><div class="stack"><button class="ghost" on:click={openDataDirectory} disabled={busy}>Open directory</button><button class="ghost" on:click={exportProfile}>Export JSON</button><button class="ghost" on:click={importProfile}>Import JSON</button></div><div class="update-check"><h2>Application updates</h2><p>{#if updateInfo}Version {updateInfo.currentVersion} is {updateInfo.updateAvailable?'out of date':'up to date'}.{:else}The app checks GitHub Releases when it starts.{/if}</p><button class="ghost" on:click={()=>checkForUpdates(true)} disabled={checkingForUpdates}>{checkingForUpdates?'Checking…':'Check for updates'}</button></div></article></div>
      </section>
    {/if}
  </main>
</div>

{#if showProcessStart}
<Modal label="Start process" onClose={()=>showProcessStart=false}>
  <form class="modal wide" on:submit|preventDefault={startProcess}>
    <div class="modal-title"><h2 id="start-process-title">Start process</h2><button type="button" class="icon-btn" aria-label="Close start process" on:click={()=>showProcessStart=false}><X size={18}/></button></div>
    <div class="start-process-context"><strong>{selectedProcess?.name||processId}</strong><code>{processId}</code><span>{currentProfile?.name} · {selectedDefinition?.version||processVersion ? `Version ${selectedDefinition?.version||processVersion}` : 'Latest deployed version'}</span></div>
    <div class="editor-field"><div class="editor-field-head"><span class="field-label">JSON variables</span><button type="button" class="link" on:click={formatProcessVariables} disabled={busy||startingProcess}>Format JSON</button></div><JsonEditor bind:value={processVariables} ariaLabel="Process variables"/></div>
          {#if processError}<div class="inline-error" role="alert">{processError}</div>{/if}
          {#if processResult}<div class="process-result" role="status"><h3>Process started</h3><p>{processResultProfile} · {processResult.bpmnProcessId} · version {processResult.version}</p><dl><dt>Process instance key</dt><dd><code>{processResult.processInstanceKey}</code></dd><dt>Process definition key</dt><dd><code>{processResult.processDefinitionKey}</code></dd></dl></div>{/if}

    <div class="modal-actions"><span></span><button type="button" class="ghost" on:click={()=>showProcessStart=false}>{processResult?'Close':'Cancel'}</button><button class="worker-start with-icon" disabled={busy||startingProcess||!selectedDefinition}><Play size={14}/>{startingProcess?'Starting…':processResult?'Start another instance':'Start process'}</button></div>
  </form>
</Modal>
{/if}

{#if editingType}<Modal label="Job type settings" onClose={()=>editingType=null}><form class="modal" on:submit|preventDefault={saveType}><div class="modal-title"><div><h2>{editingType.id?'Edit':'New'} job type</h2></div><div class="actions">{#if editingType.id}<ActionMenu><button type="button" class="danger-text" disabled={busy} on:click={()=>requestDeleteType(editingType!)}>Delete job type…</button></ActionMenu>{/if}<button type="button" class="icon-btn" aria-label="Close" on:click={()=>editingType=null}><X size={18}/></button></div></div><label>Job type<input data-modal-initial-focus bind:value={editingType.jobType} placeholder="for example, send-email" required/></label><label>Description<textarea class="short" bind:value={editingType.description}></textarea></label><div class="two"><label>Mode<select bind:value={editingType.mode} on:change={()=>{if(editingType&&!editingType.id)editingType.maxActiveJobs=editingType.mode==='manual'?1:5}}><option value="manual">Manual</option><option value="auto">Automatic</option></select></label><label>Activation limit<input type="number" min="1" bind:value={editingType.maxActiveJobs}/></label></div>{#if modalOperation?.owner===editingType}{#if modalOperation.error}<div class="inline-error" role="alert">{modalOperation.error}</div>{/if}{#if modalOperation.label}<p role="status" class="hint">{modalOperation.label}</p>{/if}{/if}<div class="modal-actions"><span></span><button type="button" class="ghost" on:click={()=>editingType=null}>Cancel</button><button class="primary" disabled={busy}>{modalOperation?.owner===editingType&&modalOperation.label==='Saving…'?'Saving…':'Save'}</button></div></form></Modal>{/if}

{#if editingScenario}<Modal label="Response scenario" onClose={()=>editingScenario=null}><form class="modal wide" on:submit|preventDefault={saveScenario}><div class="modal-title"><div><h2>{editingScenario.id?'Edit':'New'} response scenario</h2></div><div class="actions">{#if editingScenario.id}<ActionMenu><button type="button" disabled={busy} on:click={()=>duplicateScenario(editingScenario!.id)}>Duplicate response</button><button type="button" class="danger-text" disabled={busy} on:click={()=>requestDeleteScenario(editingScenario!)}>Delete response…</button></ActionMenu>{/if}<button type="button" class="icon-btn" aria-label="Close" on:click={()=>editingScenario=null}><X size={18}/></button></div></div><p class="hint">Shared response: changes apply everywhere this job type is used in this connection.</p><div class="two"><label>Name<input data-modal-initial-focus bind:value={editingScenario.name} required/></label><label>Outcome<select bind:value={editingScenario.outcome}><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select></label></div><label>Description<input bind:value={editingScenario.description}/></label><div class="editor-field"><div class="editor-field-head"><span class="field-label">JSON variables</span><button type="button" class="link" disabled={busy} on:click={formatScenario}>Format JSON</button></div><JsonEditor bind:value={editingScenario.variablesJson} ariaLabel="Scenario JSON variables"/></div><label>Automatic response delay, ms<input type="number" min="0" bind:value={editingScenario.delayMs}/></label>{#if editingScenario.outcome==='business_error'}<div class="two"><label>errorCode<input bind:value={editingScenario.errorCode} required/></label><label>errorMessage<input bind:value={editingScenario.errorMessage}/></label></div>{:else if editingScenario.outcome==='technical_failure'}<label>errorMessage<input bind:value={editingScenario.errorMessage}/></label><div class="two"><label>remainingRetries<input type="number" min="0" bind:value={editingScenario.remainingRetries}/></label><label>retryBackoffMs<input type="number" min="0" bind:value={editingScenario.retryBackoffMs}/></label></div><small class="hint">This is the absolute retry count sent to the server, not a decrement command.</small>{/if}{#if modalOperation?.owner===editingScenario}{#if modalOperation.error}<div class="inline-error" role="alert">{modalOperation.error}</div>{/if}{#if modalOperation.label}<p role="status" class="hint">{modalOperation.label}</p>{/if}{/if}<div class="modal-actions"><span></span><button type="button" class="ghost" on:click={()=>editingScenario=null}>Cancel</button><button class="primary" disabled={busy}>{modalOperation?.owner===editingScenario&&modalOperation.label==='Saving…'?'Saving…':'Save'}</button></div></form></Modal>{/if}

{#if editingProfile}<Modal label="Connection profile" onClose={closeProfile}><form class="modal wide" on:submit|preventDefault={saveProfile}><div class="modal-title"><div><h2>{editingProfile.id?'Edit connection profile':'New connection profile'}</h2></div><button type="button" class="icon-btn" aria-label="Close" on:click={closeProfile}><X size={18}/></button></div><label>Name<input data-modal-initial-focus bind:value={editingProfile.name} required/></label><div class="two"><label>Host<input bind:value={editingProfile.host} required/></label><label>Port<input type="number" min="1" max="65535" bind:value={editingProfile.port}/></label></div><label>Operate URL (optional)<input type="url" bind:value={editingProfile.operateUrl} placeholder="http://localhost:8081"/></label><small class="hint">Used to list deployed processes. Enter the Operate base URL for this Zeebe cluster, with or without /v1.</small><label>Operate authentication<select bind:value={editingOperateAuth.mode} on:change={()=>editingOperateAuth={...emptyOperateAuth(),mode:editingOperateAuth.mode}}><option value="none">None</option><option value="password">Username and password</option><option value="token">Bearer token</option></select></label>
{#if editingOperateAuth.mode==='password'}<div class="two"><label>Operate username<input autocomplete="off" bind:value={editingOperateAuth.username} required/></label><label>Operate password<input type="password" autocomplete="off" bind:value={editingOperateAuth.password} required/></label></div><small class="hint">For Operate’s built-in login. Identity/SSO deployments may require a bearer token.</small>
{:else if editingOperateAuth.mode==='token'}<label>Operate bearer token<input type="password" autocomplete="off" bind:value={editingOperateAuth.token} required/></label>{/if}
<small class="hint">Credentials are saved in the local SQLite database and restored when the app opens. Exports do not include credentials.</small><label>Camunda version<input list="camunda-versions" bind:value={editingProfile.selectedVersion} pattern="8\.[0-9]+(\.[0-9]+)?" placeholder="8.5 or 8.x.y" required/><datalist id="camunda-versions"><option value="8.5">Verified</option><option value="8.6">Compatibility not verified</option><option value="8.7">Compatibility not verified</option><option value="8.8">Compatibility not verified</option></datalist></label>{#if editingProfile.selectedVersion!=='8.5'}<small class="hint warn-text">API 8.5 compatibility mode. Compatibility has not been verified.</small>{/if}<div class="three"><label>Command, ms<input type="number" min="1" bind:value={editingProfile.commandTimeoutMs}/></label><label>Activation, ms<input type="number" min="1" bind:value={editingProfile.activationTimeoutMs}/></label><label>Renewal, ms<input type="number" min="1" bind:value={editingProfile.renewalIntervalMs}/></label></div><div class="two"><label>Global limit<input type="number" min="1" bind:value={editingProfile.maxActiveJobs}/></label><label>History retention, days<input type="number" min="1" bind:value={editingProfile.historyRetentionDays}/></label></div>{#if modalOperation?.owner===editingProfile}{#if modalOperation.error}<div class="inline-error" role="alert">{modalOperation.error}</div>{/if}{#if modalOperation.label}<p role="status" class="hint">{modalOperation.label}</p>{/if}{/if}<div class="modal-actions"><span></span><button type="button" class="ghost" on:click={closeProfile}>Cancel</button><button class="primary" disabled={busy}>{modalOperation?.owner===editingProfile&&modalOperation.label==='Saving…'?'Saving…':'Save'}</button></div></form></Modal>{/if}

{#if selectedHistory}<Modal label="Activation history" onClose={()=>selectedHistory=null}><div class="modal wide history-detail"><div class="modal-title"><div><h2 tabindex="-1" data-modal-initial-focus>{selectedHistory.jobType} activation</h2></div><button class="icon-btn" aria-label="Close" on:click={()=>selectedHistory=null}><X size={18}/></button></div><div class="key-grid"><span>Job key<code>{selectedHistory.jobKey}</code></span><span>Process instance<code>{selectedHistory.processInstanceKey}</code></span><span>Received<b>{fmtDate(selectedHistory.receivedAt)}</b></span><span>Activation<b>{selectedHistory.activationState}</b></span></div>{#if selectedHistory.activationStateReason}<div class="inline-error">{selectedHistory.activationStateReason}</div>{/if}<h3>Input variables</h3><pre>{selectedHistory.inputJson}</pre><h3>Send attempts</h3>{#if attemptsLoading}<p role="status">Loading send attempts…</p>{:else if attemptsError}<div class="inline-error" role="alert">{attemptsError} <button class="link" on:click={()=>selectedHistory&&openHistory(selectedHistory)}>Retry</button></div>{:else if !attempts.length}<p>No command has been sent.</p>{/if}{#each attempts as item}<div class="attempt"><b>#{item.sequence} · {outcomeLabel(item.command)}</b><span class="send {item.status}">{sendLabel(item.status)}</span><small>{item.durationMs} ms · {item.diagnosticCode} {item.diagnosticText}</small><pre>{item.payloadJson}</pre></div>{/each}</div></Modal>{/if}

{#if confirmation}<Modal label={confirmation.title} onClose={()=>confirmation=null} role="alertdialog"><div class="modal confirm-dialog"><div class="modal-title"><h2 id="confirm-title">{confirmation.title}</h2><button class="icon-btn" aria-label="Close" on:click={()=>confirmation=null}><X size={18}/></button></div>{#if confirmation.target}<code class="confirm-target">{confirmation.target}</code>{/if}<p>{confirmation.message}</p>{#if modalOperation?.owner===confirmation}{#if modalOperation.error}<div class="inline-error" role="alert">{modalOperation.error}</div>{/if}{#if modalOperation.label}<p role="status" class="hint">{modalOperation.label}</p>{/if}{/if}<div class="modal-actions"><span></span><button data-modal-initial-focus class="ghost" on:click={()=>confirmation=null}>Cancel</button><button class="destructive" on:click={confirmAction} disabled={busy}>{modalOperation?.owner===confirmation&&modalOperation.label?modalOperation.label:confirmation.confirmLabel}</button></div></div></Modal>{/if}
{/if}
