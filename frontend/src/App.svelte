<script lang="ts">
  import { onMount } from 'svelte'
  import { Boxes, Cable, Check, ChevronDown, ChevronLeft, ChevronRight, Clock3, Download, Ellipsis, History, Plus, Play, Square, Waypoints, X } from '@lucide/svelte'
  import { call, on } from './lib/api'
  import JsonEditor from './lib/JsonEditor.svelte'

  type Profile = { operateAuthMode:OperateAuth['mode']; operateUsername?:string; operatePassword?:string; operateToken?:string; operateUrl:string; id:string; name:string; host:string; port:number; selectedVersion:string; compatibilityVerified:boolean; commandTimeoutMs:number; activationTimeoutMs:number; renewalIntervalMs:number; maxActiveJobs:number; historyRetentionDays:number }
  type JobType = { id:string; profileId:string; jobType:string; description:string; mode:'manual'|'auto'; activeScenarioId:string|null; maxActiveJobs:number }
  type Scenario = { id:string; jobTypeConfigId:string; name:string; description:string; outcome:'success'|'business_error'|'technical_failure'; variablesJson:string; delayMs:number; errorCode:string; errorMessage:string; remainingRetries:number; retryBackoffMs:number }
  type RuntimeState = { configId:string; state:string; activeJobs:number; lastError:string }
  type Activation = { id:string; jobTypeConfigId:string; jobKey:string; jobType:string; mode:'manual'|'auto'; processInstanceKey:string; bpmnProcessId:string; elementId:string; retries:number; inputJson:string; customHeadersJson:string; scenarioSnapshotJson:string; draftJson:string; sendStatus:string; activationState:string; activationStateReason:string; receivedAt:string }
  type Draft = { outcome:'success'|'business_error'|'technical_failure'; variablesJson:string; errorCode:string; errorMessage:string; remainingRetries:number; retryBackoffMs:number }
  type Confirmation = { title:string; message:string; target?:string; confirmLabel:string; success:string; action:()=>Promise<void> }
  type UpdateInfo = { currentVersion:string; latestVersion:string; updateAvailable:boolean; releaseUrl:string }

  let tab:'types'|'process'|'pending'|'history'|'settings' = 'types'
  type ProcessInstance = { bpmnProcessId:string; version:number; processDefinitionKey:string; processInstanceKey:string }
  type DeployedProcess = { bpmnProcessId:string; name:string; latestVersion:number }
  let deployedProcesses:DeployedProcess[] = [], loadingProcesses = false, processesLoaded = false, processesError = '', manualProcessId = false, processSourceKey = '', processListRequest = 0
  type OperateAuth = { mode:'none'|'token'|'password'; token:string; username:string; password:string }
  const emptyOperateAuth = ():OperateAuth => ({mode:'none',token:'',username:'',password:''})
  let editingOperateAuth = emptyOperateAuth()
  let processId = '', processVersion:number|undefined, processVariables = '{}'
  let startingProcess = false, processError = '', processResult:ProcessInstance|null = null, processResultProfile = ''
  let loading = true, busy = false, notice = '', error = ''
  let noticeTimer:number|undefined, errorTimer:number|undefined
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
  let confirmation:Confirmation|null = null
  let drafts:Record<string,Draft> = {}
  let draftScenarios:Record<string,string> = {}
  let history:any = { items:[], page:1, pageSize:100, total:0 }, historySearch = '', historyStatus = '', historyOutcome = '', historyType = '', historyFrom = '', historyTo = '', historyPage = 1, selectedHistory:Activation|null = null, attempts:any[] = []
  $: currentProfile = profiles.find(p=>p.id===selectedProfileId)
  $: profileDeletionReasons = Object.fromEntries(profiles.map(profile=>[profile.id,profileDeletionReason(profile,profiles.length,selectedProfileId,runtime,pending.length)]))
  $: if (currentProfile && processSourceKey !== `${currentProfile.id}|${currentProfile.operateUrl||''}`) resetProcessSource(`${currentProfile.id}|${currentProfile.operateUrl||''}`)
  $: if (tab==='process' && currentProfile?.operateUrl && !processesLoaded && !loadingProcesses) void loadProcesses()
  $: currentViewLabel = ({types:'Job types',process:'Start process',pending:'Awaiting response',history:'History',settings:'Connections'} as const)[tab]

  const outcomeLabel = (v:string) => ({success:'Success',business_error:'Business error',technical_failure:'Technical failure'} as any)[v] || v
  const sendLabel = (v:string) => ({not_prepared:'Not prepared',prepared:'Response prepared',sending:'Sending',confirmed:'Confirmed by Camunda',failed:'Send failed',unknown:'Outcome unknown'} as any)[v] || v
  const stateLabel = (v:string) => ({stopped:'Stopped',running:'Running',stopping:'Stopping',error:'Error'} as any)[v] || v
  const rt = (id:string):RuntimeState => runtime.find(x=>x.configId===id) || {configId:id,state:'stopped',activeJobs:0,lastError:''}
  const scenarioName = (id:string|null) => scenarios.find(x=>x.id===id)?.name || 'Not selected'
  const fmtDate = (v:string) => v ? new Date(v).toLocaleString('en-US') : '—'
  const parseDraft = (a:Activation):Draft => { try { return JSON.parse(a.draftJson) } catch { return {outcome:'success',variablesJson:'{}',errorCode:'',errorMessage:'',remainingRetries:0,retryBackoffMs:0} } }

  const notificationDurationMs = 5000
  function showNotice(message:string) { notice=message; if(noticeTimer)window.clearTimeout(noticeTimer);noticeTimer=message?window.setTimeout(()=>{notice='';noticeTimer=undefined},notificationDurationMs):undefined }
  function showError(message:string) { error=message; if(errorTimer)window.clearTimeout(errorTimer);errorTimer=message?window.setTimeout(()=>{error='';errorTimer=undefined},notificationDurationMs):undefined }

  async function run(fn:()=>Promise<any>, success='') { showError(''); busy=true; try { await fn(); if(success) showNotice(success) } catch(e:any) { showError(e?.message || String(e)) } finally { busy=false } }
  async function reload() { const b = await call<any>('Bootstrap'); profiles=b.profiles||[]; selectedProfileId=b.selectedProfileId; jobTypes=b.jobTypes||[]; scenarios=b.scenarios||[]; runtime=b.runtime||[]; connection=b.connection; pending=b.pending||[]; dataPath=b.dataPath; for(const a of pending) if(!drafts[a.id]) { drafts[a.id]=parseDraft(a); try { draftScenarios[a.id]=JSON.parse(a.scenarioSnapshotJson).id||'' } catch { draftScenarios[a.id]='' } } loading=false }
  async function refreshLiveState() { if(refreshingLiveState)return;refreshingLiveState=true;try{const b=await call<any>('Bootstrap');runtime=b.runtime||[];connection=b.connection;pending=b.pending||[];for(const a of pending)if(!drafts[a.id]){drafts[a.id]=parseDraft(a);try{draftScenarios[a.id]=JSON.parse(a.scenarioSnapshotJson).id||''}catch{draftScenarios[a.id]=''}}}catch{}finally{refreshingLiveState=false} }
  async function selectProfile() { await run(async()=>{ await call('SelectProfile',selectedProfileId); await reload() }) }
  async function checkConnection() { await run(async()=>{ connection=await call('CheckConnection') },'Connection check complete') }
  function resetProcessSource(key:string){
    processSourceKey=key;processListRequest++;deployedProcesses=[];processesLoaded=false;loadingProcesses=false;processesError='';processId='';processVersion=undefined;manualProcessId=false
  }
  async function loadProcesses(){
    const profileId=selectedProfileId, request=++processListRequest
    loadingProcesses=true;processesError='';processesLoaded=true
    try {
      const result=await call<DeployedProcess[]>('ListProcesses',profileId)
      if(request!==processListRequest)return
      deployedProcesses=result||[]
      if(!manualProcessId&&!deployedProcesses.some(p=>p.bpmnProcessId===processId))processId=''
    } catch(e:any) {if(request===processListRequest){deployedProcesses=[];processesError=e?.message||String(e)}}
    finally {if(request===processListRequest)loadingProcesses=false}
  }
  async function startProcess(){
    if(startingProcess||busy)return
    startingProcess=true;processError='';processResult=null
    processResultProfile=currentProfile?.name||selectedProfileId
    try {
      processResult=await call<ProcessInstance>('StartProcess',{profileId:selectedProfileId,bpmnProcessId:processId,version:processVersion??0,variablesJson:processVariables})
    } catch(e:any) { processError=e?.message||String(e) } finally { startingProcess=false }
  }
  async function formatProcessVariables(){await run(async()=>{processVariables=await call('FormatJSON',processVariables)})}
  async function startAll(){await run(async()=>{await call('StartAll');await reload()},'Workers started')}
  async function stopAll(){await run(async()=>{await call('StopAll');await reload()},'Workers stopped')}
  async function startType(id:string){await run(async()=>{await call('StartType',id);await reload()},'Worker started')}
  async function stopType(id:string){await run(async()=>{await call('StopType',id);await reload()},'Worker stopped')}

  function newType(){ editingType={id:'',profileId:selectedProfileId,jobType:'',description:'',mode:'manual',activeScenarioId:null,maxActiveJobs:1} }
  async function saveType(){if(!editingType)return;await run(async()=>{await call('SaveJobType',editingType);editingType=null;await reload()},'Job type saved')}
  function requestDeleteType(type:JobType){confirmation={title:'Delete job type?',message:'Its response scenarios will also be deleted. Activation history will be preserved.',target:type.jobType,confirmLabel:'Delete job type',success:'Job type deleted',action:async()=>{await call('DeleteJobType',type.id);editingType=null;await reload()}}}
  function newScenario(typeId:string){editingScenario={id:'',jobTypeConfigId:typeId,name:'',description:'',outcome:'success',variablesJson:'{}',delayMs:0,errorCode:'',errorMessage:'',remainingRetries:0,retryBackoffMs:0}}
  async function saveScenario(){if(!editingScenario)return;await run(async()=>{await call('SaveScenario',editingScenario);editingScenario=null;await reload()},'Scenario saved')}
  async function duplicateScenario(id:string){await run(async()=>{await call('DuplicateScenario',id);await reload()},'Scenario duplicated')}
  function requestDeleteScenario(scenario:Scenario){confirmation={title:'Delete response?',message:'This response scenario will be removed from its job type.',target:scenario.name,confirmLabel:'Delete response',success:'Scenario deleted',action:async()=>{await call('DeleteScenario',scenario.id);editingScenario=null;await reload()}}}
  async function setActive(type:JobType,value:string){type.activeScenarioId=value||null;await run(async()=>{await call('SaveJobType',type);await reload()})}
  async function formatScenario(){if(!editingScenario)return;await run(async()=>{editingScenario!.variablesJson=await call('FormatJSON',editingScenario!.variablesJson)})}

  async function submit(a:Activation){const d=drafts[a.id];await run(async()=>{await call('SaveDraft',a.id,d);await call('SubmitResponse',a.id,d);await reload()},'Response confirmed by Camunda')}
  async function saveDraft(a:Activation){await run(async()=>{await call('SaveDraft',a.id,drafts[a.id])},'Draft saved')}
  async function formatDraft(id:string){await run(async()=>{drafts[id].variablesJson=await call('FormatJSON',drafts[id].variablesJson)})}
  function draftFromScenario(s:Scenario):Draft {
    return {outcome:s.outcome,variablesJson:s.variablesJson,errorCode:s.errorCode,errorMessage:s.errorMessage,remainingRetries:s.remainingRetries,retryBackoffMs:s.retryBackoffMs}
  }
  async function applyDraftScenario(a:Activation,id:string){
    if(!id)return
    draftScenarios[a.id]=id
    const scenario=scenarios.find(s=>s.id===id&&s.jobTypeConfigId===a.jobTypeConfigId)
    if(!scenario)return
    const previous=drafts[a.id]
    drafts[a.id]=draftFromScenario(scenario)
    showError('')
    try {
      const saved=await call<Draft>('ApplyScenario',a.id,id)
      if(draftScenarios[a.id]===id)drafts[a.id]=saved
    } catch(e:any) {
      if(draftScenarios[a.id]===id)drafts[a.id]=previous
      showError(e?.message||String(e))
    }
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
  async function openHistory(a:Activation){selectedHistory=a;attempts=await call('Attempts',a.id)}
  function requestClearHistory(){confirmation={title:'Clear completed history?',message:'Completed activation records for this profile will be deleted. Active records will be preserved.',confirmLabel:'Clear history',success:'History cleared',action:async()=>{await call('ClearHistory',selectedProfileId,true);await loadHistory(1)}}}
  async function confirmAction(){const pending=confirmation;if(!pending)return;await run(async()=>{await pending.action();confirmation=null},pending.success)}

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
    confirmation={title:'Delete connection profile?',message:'Its saved credentials, job types, and response scenarios will also be deleted. Activation history will remain in the local database, but will no longer be accessible through this profile. This cannot be undone.',target:profile.name,confirmLabel:'Delete profile',success:'Connection profile deleted',action:async()=>{await call('DeleteProfile',profile.id);await reload()}}
  }
  async function saveProfile(){if(!editingProfile)return;await run(async()=>{const auth={...editingOperateAuth};const saved=await call<Profile>('SaveProfile',{...editingProfile,operateAuthMode:auth.mode,operateUsername:auth.username,operatePassword:auth.password,operateToken:auth.token});closeProfile();if(!selectedProfileId){selectedProfileId=saved.id;await call('SelectProfile',saved.id)};await reload();if(saved.id===selectedProfileId){processListRequest++;deployedProcesses=[];processesError='';processesLoaded=false;loadingProcesses=false;}},'Profile saved')}
  async function openDataDirectory(){await run(async()=>{await call('OpenDataDirectory')})}
  async function exportProfile(){await run(async()=>{await call('ExportProfileFile',selectedProfileId)},'Profile exported')}
  async function importProfile(){await run(async()=>{await call('ImportProfileFile');await reload()},'Profile imported')}
  async function checkForUpdates(manual=false){
    if(checkingForUpdates)return
    checkingForUpdates=true
    try {
      updateInfo=await call<UpdateInfo>('CheckForUpdates')
      if(manual&&!updateInfo.updateAvailable)showNotice(`Camunda Stub Worker ${updateInfo.currentVersion} is up to date`)
    } catch(e:any) {
      if(manual)showError(e?.message||String(e))
    } finally {
      checkingForUpdates=false
    }
  }
  async function openReleasesPage(){await call('OpenReleasesPage')}

  onMount(()=>{const offs=[on('runtime:changed',(v)=>runtime=v),on('connection:changed',(v)=>connection=v),on('activation:created',(a)=>{if(a.mode==='manual'){pending=[...pending,a];drafts[a.id]=parseDraft(a)}queueHistoryRefresh()}),on('activation:changed',(a)=>{const keep=a.mode==='manual'&&a.activationState==='active';pending=keep?(pending.some(x=>x.id===a.id)?pending.map(x=>x.id===a.id?a:x):[...pending,a]):pending.filter(x=>x.id!==a.id);queueHistoryRefresh()}),on('storage:error',(v)=>showError('SQLite: '+v))];const liveTimer=window.setInterval(()=>void refreshLiveState(),1500);void reload().then(()=>checkForUpdates()).catch((e:any)=>{showError(e?.message||String(e));loading=false});return()=>{window.clearInterval(liveTimer);if(historyRefreshTimer!==undefined)window.clearTimeout(historyRefreshTimer);if(noticeTimer)window.clearTimeout(noticeTimer);if(errorTimer)window.clearTimeout(errorTimer);offs.forEach(f=>f())}})
</script>

<svelte:head><title>Camunda Stub Worker</title></svelte:head>

{#if loading}<div class="splash"><span class="spinner"></span>Opening local database…</div>{:else}
<div class="app-shell">
  <aside>
    <div class="brand"><Waypoints size={17}/><strong>Stub Worker</strong></div>
    <nav>
      <button class:active={tab==='types'} on:click={()=>tab='types'}><span><Boxes size={18}/></span> Job types</button>
      <button class:active={tab==='pending'} on:click={()=>tab='pending'}><span><Clock3 size={18}/></span> Awaiting response <b>{pending.length}</b></button>
      <button class:active={tab==='process'} on:click={()=>tab='process'}><span><Play size={18}/></span> Start process</button>
      <button class:active={tab==='history'} on:click={()=>{tab='history';loadHistory(1)}}><span><History size={18}/></span> History</button>
      <button class:active={tab==='settings'} on:click={()=>tab='settings'}><span><Cable size={18}/></span> Connections</button>
    </nav>
    <div class="sidebar-note"><i></i><div><strong>{connection.state==='connected'?'Camunda available':'No connection'}</strong><small>{connection.message}</small></div></div>
  </aside>
  <main>
    <header>
      <strong class="view-title">{currentViewLabel}</strong>
      <div class="profile-select"><label for="profile-select">Profile</label><select id="profile-select" bind:value={selectedProfileId} on:change={selectProfile} disabled={busy||startingProcess}>{#each profiles as p}<option value={p.id}>{p.name}</option>{/each}</select></div>
      <div class="connection"><span class:ok={connection.state==='connected'} class:warn={connection.state==='version_mismatch'}></span><div><strong>{connection.state==='connected'?'Connected':connection.state==='version_mismatch'?'Versions differ':'Not connected'}</strong><small>{connection.detectedVersion ? `Server ${connection.detectedVersion} · selected ${connection.selectedVersion}` : `${profiles.find(p=>p.id===selectedProfileId)?.host}:${profiles.find(p=>p.id===selectedProfileId)?.port}`}</small></div></div>
      <button class="ghost" on:click={checkConnection} disabled={busy}>Check connection</button>
    </header>

    {#if updateInfo?.updateAvailable}<div class="update-banner" role="status"><Download size={18}/><div><strong>Camunda Stub Worker {updateInfo.latestVersion} is available</strong><small>You are using {updateInfo.currentVersion}. Download the new release when convenient.</small></div><button class="secondary" on:click={openReleasesPage}>View release</button><button class="icon-btn" aria-label="Dismiss update notification" on:click={()=>updateInfo=null}><X size={17}/></button></div>{/if}

    {#if error || notice}<div class="toast-region" aria-live="polite">
      {#if error}<div class="toast error" role="alert"><span>{error}</span><button aria-label="Dismiss error" on:click={()=>showError('')}><X size={16}/></button></div>{/if}
      {#if notice}<div class="toast success"><span>{notice}</span><button aria-label="Dismiss notification" on:click={()=>showNotice('')}><X size={16}/></button></div>{/if}
    </div>{/if}

    {#if tab==='types'}
      <section class="workspace">
        <div class="command-bar"><strong>{jobTypes.length} {jobTypes.length===1?'job type':'job types'}</strong><div class="actions"><button class="secondary with-icon" on:click={newType}><Plus size={14}/> Add</button>{#if jobTypes.length>1}{#if jobTypes.some(x=>rt(x.id).state==='running'||rt(x.id).state==='stopping')}<button class="ghost with-icon" on:click={stopAll}><Square size={12}/> Stop all</button>{/if}<button class="ghost with-icon" on:click={startAll} disabled={busy}><Play size={12}/> Start all</button>{/if}</div></div>
        {#if !jobTypes.length}<div class="empty"><div><Boxes size={24} strokeWidth={1.5}/></div><h3>No job types</h3><p>Add the exact job type from your BPMN model, then define its responses.</p><button class="secondary with-icon" on:click={newType}><Plus size={14}/> Add job type</button></div>{/if}
        {#if jobTypes.length}<div class="worker-table"><div class="worker-columns" aria-hidden="true"><span>Job type</span><span>Mode</span><span>Scenario</span><span>Active</span><span>State</span><span></span></div>
        {#each jobTypes as type}
          <article class="worker-row">
            <div class="worker-main"><div class="worker-identity"><h3>{type.jobType}</h3>{#if type.description}<p>{type.description}</p>{/if}</div><span class="worker-value">{type.mode==='auto'?'Automatic':'Manual'}</span><span class="meta-select"><select aria-label="Active scenario" value={type.activeScenarioId||''} on:change={(e)=>setActive(type,(e.currentTarget as HTMLSelectElement).value)}><option value="">Not selected</option>{#each scenarios.filter(s=>s.jobTypeConfigId===type.id) as s}<option value={s.id}>{s.name}</option>{/each}</select><ChevronDown size={12}/></span><span class="worker-value mono">{rt(type.id).activeJobs} / {type.maxActiveJobs}</span><span class="status {rt(type.id).state}">{stateLabel(rt(type.id).state)}</span><div class="worker-actions">{#if rt(type.id).state==='running'||rt(type.id).state==='stopping'}<button class="danger-text" on:click={()=>stopType(type.id)}>Stop</button>{:else}<button class="row-action" on:click={()=>startType(type.id)} disabled={busy}>Start</button>{/if}<button class="icon-btn" aria-label="Edit job type" title="Edit" on:click={()=>editingType={...type}}><Ellipsis size={17}/></button></div></div>
            {#if rt(type.id).lastError}<div class="inline-error">{rt(type.id).lastError}</div>{/if}
            <div class="worker-responses"><span class="responses-label">Responses</span>{#each scenarios.filter(s=>s.jobTypeConfigId===type.id) as s}<button class="response-link" class:chosen={s.id===type.activeScenarioId} on:click={()=>editingScenario={...s}}><i class={s.outcome}></i><span>{s.name}</span><small>{outcomeLabel(s.outcome)}</small></button>{/each}<button class="add-response" on:click={()=>newScenario(type.id)}><Plus size={12}/> Add response</button></div>
          </article>
        {/each}</div>{/if}
      </section>
    {:else if tab==='process'}
      <section class="workspace">
        <form class="process-form" on:submit|preventDefault={startProcess}>
          <div><h2>Start a process instance</h2></div>
          <div class="two">
            <label>BPMN process ID
              {#if currentProfile?.operateUrl && !manualProcessId}
                <select bind:value={processId} required disabled={startingProcess||loadingProcesses} on:change={()=>processVersion=undefined}><option value="">{loadingProcesses?'Loading processes…':'Select a deployed process'}</option>{#each deployedProcesses as process}<option value={process.bpmnProcessId}>{process.name ? `${process.name} — ` : ''}{process.bpmnProcessId} (v{process.latestVersion})</option>{/each}</select>
              {:else}<input bind:value={processId} placeholder="for example, order_process" required disabled={startingProcess}/>{/if}
            </label>
            <label>Version (optional)<input type="number" min="1" max="2147483647" step="1" bind:value={processVersion} placeholder="Latest deployed version" disabled={startingProcess}/></label>
          </div>
          {#if currentProfile?.operateUrl}
            <div class="actions"><button type="button" class="ghost" on:click={loadProcesses} disabled={loadingProcesses||startingProcess}>{loadingProcesses?'Loading…':'Refresh processes'}</button><button type="button" class="link" on:click={()=>manualProcessId=!manualProcessId} disabled={startingProcess}>{manualProcessId?'Choose from deployed processes':'Enter ID manually'}</button></div>
            <p class="hint">Uses the Operate authentication configured in <button type="button" class="link" on:click={editCurrentProfile}>connection settings</button>.</p>
            {#if processesError}<div class="inline-error" role="alert">{processesError} You can still enter a process ID manually.</div>{:else if processesLoaded&&!loadingProcesses&&!deployedProcesses.length}<p class="hint">No deployed processes found in the default tenant. Recently deployed models may take a moment to appear in Operate.</p>{/if}
          {:else}<p class="hint">To choose from deployed processes, <button type="button" class="link" on:click={editCurrentProfile}>set the Operate URL in your connection profile</button>.</p>{/if}
          <div class="editor-field"><div class="editor-field-head"><span class="field-label">JSON variables</span><button type="button" class="link" on:click={formatProcessVariables} disabled={busy||startingProcess}>Format JSON</button></div><JsonEditor bind:value={processVariables} ariaLabel="Process variables"/></div>
          <div class="submit-row"><button class="primary with-icon" disabled={busy||startingProcess||!processId.trim()}><Play size={14}/>{startingProcess?'Starting…':'Start process'}</button></div>
          {#if processError}<div class="inline-error" role="alert">{processError}</div>{/if}
          {#if processResult}<div class="process-result" role="status"><h3>Process started</h3><p>{processResultProfile} · {processResult.bpmnProcessId} · version {processResult.version}</p><dl><dt>Process instance key</dt><dd><code>{processResult.processInstanceKey}</code></dd><dt>Process definition key</dt><dd><code>{processResult.processDefinitionKey}</code></dd></dl></div>{/if}
        </form>
      </section>
    {:else if tab==='pending'}
      <section class="workspace">
      {#if !pending.length}<div class="empty"><div><Check size={24} strokeWidth={1.7}/></div><h3>No jobs awaiting response</h3><p>Jobs activated by manual workers will appear here.</p></div>{/if}
      {#each pending as a (a.id)}
        <article class="pending-card"><div class="pending-head"><div><span class="pill">{a.jobType}</span><h3>Job key <code>{a.jobKey}</code></h3><p>{a.bpmnProcessId} · {a.elementId} · received {fmtDate(a.receivedAt)}</p></div><span class="status running">Activation valid</span></div>
          <div class="json-grid"><div><span class="field-label">Input variables</span><pre>{a.inputJson}</pre><details><summary>Custom headers</summary><pre>{a.customHeadersJson}</pre></details></div><div><label>Scenario<select value={draftScenarios[a.id]||''} on:change={(e)=>applyDraftScenario(a,(e.currentTarget as HTMLSelectElement).value)}><option value="">Select a scenario</option>{#each scenarios.filter(s=>s.jobTypeConfigId===a.jobTypeConfigId) as s}<option value={s.id}>{s.name}</option>{/each}</select></label><label>Outcome<select bind:value={drafts[a.id].outcome}><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select></label><div class="draft-editor"><JsonEditor bind:value={drafts[a.id].variablesJson} ariaLabel="Prepared response JSON" compact/></div><button class="link" on:click={()=>formatDraft(a.id)}>Format JSON</button>{#if drafts[a.id].outcome==='business_error'}<div class="two"><input bind:value={drafts[a.id].errorCode} placeholder="errorCode"/><input bind:value={drafts[a.id].errorMessage} placeholder="Message"/></div>{:else if drafts[a.id].outcome==='technical_failure'}<input bind:value={drafts[a.id].errorMessage} placeholder="Failure message"/><div class="two"><label>Remaining retries<input type="number" min="0" bind:value={drafts[a.id].remainingRetries}/></label><label>Backoff, ms<input type="number" min="0" bind:value={drafts[a.id].retryBackoffMs}/></label></div><small class="hint">remainingRetries is an absolute value. Reusing the same positive value may cause repeated activations.</small>{/if}</div></div>
          <div class="submit-row"><button class="ghost" on:click={()=>saveDraft(a)} disabled={busy}>Save draft</button><button class="primary" on:click={()=>submit(a)} disabled={busy||a.sendStatus==='sending'}>Send to Camunda</button></div>
        </article>
      {/each}
      </section>
    {:else if tab==='history'}
      <section class="workspace"><div class="command-bar"><strong>{history.total||0} records</strong><button class="danger-text" on:click={requestClearHistory}>Clear completed</button></div>
      <div class="filters"><input bind:value={historySearch} placeholder="Job key or process instance key" on:keydown={(e)=>e.key==='Enter'&&loadHistory(1)}/><select bind:value={historyType}><option value="">All job types</option>{#each jobTypes as t}<option value={t.jobType}>{t.jobType}</option>{/each}</select><select bind:value={historyOutcome}><option value="">All outcomes</option><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select><select bind:value={historyStatus}><option value="">All statuses</option><option value="confirmed">Confirmed</option><option value="failed">Failed</option><option value="unknown">Unknown</option><option value="prepared">Prepared</option></select><input aria-label="From date" title="From date" type="date" bind:value={historyFrom}/><input aria-label="To date" title="To date" type="date" bind:value={historyTo}/><button class="secondary" on:click={()=>loadHistory(1)}>Search</button></div>
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

{#if editingType}<div class="modal-backdrop" role="presentation"><form class="modal" on:submit|preventDefault={saveType}><div class="modal-title"><div><h2>{editingType.id?'Edit':'New'} job type</h2></div><button type="button" class="icon-btn" aria-label="Close" on:click={()=>editingType=null}><X size={18}/></button></div><label>Job type<input bind:value={editingType.jobType} placeholder="for example, send-email" required/></label><label>Description<textarea class="short" bind:value={editingType.description}></textarea></label><div class="two"><label>Mode<select bind:value={editingType.mode} on:change={()=>{if(editingType&&!editingType.id)editingType.maxActiveJobs=editingType.mode==='manual'?1:5}}><option value="manual">Manual</option><option value="auto">Automatic</option></select></label><label>Activation limit<input type="number" min="1" bind:value={editingType.maxActiveJobs}/></label></div><div class="modal-actions">{#if editingType.id}<button type="button" class="danger-text" on:click={()=>requestDeleteType(editingType!)}>Delete job type</button>{/if}<span></span><button type="button" class="ghost" on:click={()=>editingType=null}>Cancel</button><button class="primary" disabled={busy}>Save</button></div></form></div>{/if}

{#if editingScenario}<div class="modal-backdrop"><form class="modal wide" on:submit|preventDefault={saveScenario}><div class="modal-title"><div><h2>{editingScenario.id?'Edit':'New'} response scenario</h2></div><button type="button" class="icon-btn" aria-label="Close" on:click={()=>editingScenario=null}><X size={18}/></button></div><div class="two"><label>Name<input bind:value={editingScenario.name} required/></label><label>Outcome<select bind:value={editingScenario.outcome}><option value="success">Success</option><option value="business_error">Business error</option><option value="technical_failure">Technical failure</option></select></label></div><label>Description<input bind:value={editingScenario.description}/></label><div class="editor-field"><div class="editor-field-head"><span class="field-label">JSON variables</span><button type="button" class="link" on:click={formatScenario}>Format JSON</button></div><JsonEditor bind:value={editingScenario.variablesJson} ariaLabel="Scenario JSON variables"/></div><label>Automatic response delay, ms<input type="number" min="0" bind:value={editingScenario.delayMs}/></label>{#if editingScenario.outcome==='business_error'}<div class="two"><label>errorCode<input bind:value={editingScenario.errorCode} required/></label><label>errorMessage<input bind:value={editingScenario.errorMessage}/></label></div>{:else if editingScenario.outcome==='technical_failure'}<label>errorMessage<input bind:value={editingScenario.errorMessage}/></label><div class="two"><label>remainingRetries<input type="number" min="0" bind:value={editingScenario.remainingRetries}/></label><label>retryBackoffMs<input type="number" min="0" bind:value={editingScenario.retryBackoffMs}/></label></div><small class="hint">This is the absolute retry count sent to the server, not a decrement command.</small>{/if}<div class="modal-actions">{#if editingScenario.id}<button type="button" class="danger-text" on:click={()=>requestDeleteScenario(editingScenario!)}>Delete</button><button type="button" class="ghost" on:click={()=>duplicateScenario(editingScenario!.id)}>Duplicate</button>{/if}<span></span><button type="button" class="ghost" on:click={()=>editingScenario=null}>Cancel</button><button class="primary" disabled={busy}>Save</button></div></form></div>{/if}

{#if editingProfile}<div class="modal-backdrop"><form class="modal wide" on:submit|preventDefault={saveProfile}><div class="modal-title"><div><h2>{editingProfile.id?'Edit connection profile':'New connection profile'}</h2></div><button type="button" class="icon-btn" aria-label="Close" on:click={closeProfile}><X size={18}/></button></div><label>Name<input bind:value={editingProfile.name} required/></label><div class="two"><label>Host<input bind:value={editingProfile.host} required/></label><label>Port<input type="number" min="1" max="65535" bind:value={editingProfile.port}/></label></div><label>Operate URL (optional)<input type="url" bind:value={editingProfile.operateUrl} placeholder="http://localhost:8081"/></label><small class="hint">Used to list deployed processes. Enter the Operate base URL for this Zeebe cluster, with or without /v1.</small><label>Operate authentication<select bind:value={editingOperateAuth.mode} on:change={()=>editingOperateAuth={...emptyOperateAuth(),mode:editingOperateAuth.mode}}><option value="none">None</option><option value="password">Username and password</option><option value="token">Bearer token</option></select></label>
{#if editingOperateAuth.mode==='password'}<div class="two"><label>Operate username<input autocomplete="off" bind:value={editingOperateAuth.username} required/></label><label>Operate password<input type="password" autocomplete="off" bind:value={editingOperateAuth.password} required/></label></div><small class="hint">For Operate’s built-in login. Identity/SSO deployments may require a bearer token.</small>
{:else if editingOperateAuth.mode==='token'}<label>Operate bearer token<input type="password" autocomplete="off" bind:value={editingOperateAuth.token} required/></label>{/if}
<small class="hint">Credentials are saved in the local SQLite database and restored when the app opens. Exports do not include credentials.</small><label>Camunda version<input list="camunda-versions" bind:value={editingProfile.selectedVersion} pattern="8\.[0-9]+(\.[0-9]+)?" placeholder="8.5 or 8.x.y" required/><datalist id="camunda-versions"><option value="8.5">Verified</option><option value="8.6">Compatibility not verified</option><option value="8.7">Compatibility not verified</option><option value="8.8">Compatibility not verified</option></datalist></label>{#if editingProfile.selectedVersion!=='8.5'}<small class="hint warn-text">API 8.5 compatibility mode. Compatibility has not been verified.</small>{/if}<div class="three"><label>Command, ms<input type="number" min="1" bind:value={editingProfile.commandTimeoutMs}/></label><label>Activation, ms<input type="number" min="1" bind:value={editingProfile.activationTimeoutMs}/></label><label>Renewal, ms<input type="number" min="1" bind:value={editingProfile.renewalIntervalMs}/></label></div><div class="two"><label>Global limit<input type="number" min="1" bind:value={editingProfile.maxActiveJobs}/></label><label>History retention, days<input type="number" min="1" bind:value={editingProfile.historyRetentionDays}/></label></div><div class="modal-actions"><span></span><button type="button" class="ghost" on:click={closeProfile}>Cancel</button><button class="primary" disabled={busy}>Save</button></div></form></div>{/if}

{#if selectedHistory}<div class="modal-backdrop"><div class="modal wide history-detail"><div class="modal-title"><div><h2>{selectedHistory.jobType} activation</h2></div><button class="icon-btn" aria-label="Close" on:click={()=>selectedHistory=null}><X size={18}/></button></div><div class="key-grid"><span>Job key<code>{selectedHistory.jobKey}</code></span><span>Process instance<code>{selectedHistory.processInstanceKey}</code></span><span>Received<b>{fmtDate(selectedHistory.receivedAt)}</b></span><span>Activation<b>{selectedHistory.activationState}</b></span></div>{#if selectedHistory.activationStateReason}<div class="inline-error">{selectedHistory.activationStateReason}</div>{/if}<h3>Input variables</h3><pre>{selectedHistory.inputJson}</pre><h3>Send attempts</h3>{#if !attempts.length}<p>No command has been sent.</p>{/if}{#each attempts as item}<div class="attempt"><b>#{item.sequence} · {outcomeLabel(item.command)}</b><span class="send {item.status}">{sendLabel(item.status)}</span><small>{item.durationMs} ms · {item.diagnosticCode} {item.diagnosticText}</small><pre>{item.payloadJson}</pre></div>{/each}</div></div>{/if}

{#if confirmation}<div class="modal-backdrop confirm-backdrop"><div class="modal confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title"><div class="modal-title"><h2 id="confirm-title">{confirmation.title}</h2><button class="icon-btn" aria-label="Close" on:click={()=>confirmation=null}><X size={18}/></button></div>{#if confirmation.target}<code class="confirm-target">{confirmation.target}</code>{/if}<p>{confirmation.message}</p><div class="modal-actions"><span></span><button class="ghost" on:click={()=>confirmation=null}>Cancel</button><button class="destructive" on:click={confirmAction} disabled={busy}>{confirmation.confirmLabel}</button></div></div></div>{/if}
{/if}
