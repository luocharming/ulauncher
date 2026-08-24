<script setup>
import {onMounted, reactive, ref, watch} from 'vue';
import * as monaco from 'monaco-editor';
import {updateInstanceSettings} from '@/api';
import {Notify} from 'quasar';

const props = defineProps(['row', 'sessionId']);
const config = reactive({
  name: props.row.name,
  execPath: props.row.execPath,
  enableMultiuserControl: props.row.enableMultiuserControl
});
// 分配设置表单（实例类型/白名单/连接数上限）
const alloc = reactive({
  instanceType: props.row.instanceType || 0,
  whitelistRows: (props.row.whitelist || []).map(ip => ({value: ip, error: ''})),
  maxPlayerCount: (props.row.maxPlayerCount === undefined || props.row.maxPlayerCount === null) ? -1 : props.row.maxPlayerCount
});
const dom = ref();
const labels = ref([])
let editor;
onMounted(() => {
  getLabels(props.row.labels)
  editor = monaco.editor.create(dom.value, {
    value: (props.row.launchArguments || []).join('\n'),
    language: 'ini',
    lineNumbers: 'off',
    theme: 'vs-dark',
    readOnly: true,
    minimap: {
      enabled: false // 是否启用预览图
    },
    automaticLayout: true,
    scrollBeyondLastLine: false
  });
});

watch(
    () => props.row.name,
    async (newValue, oldValue) => {
      config.name = newValue;
    }
);

watch(
    () => props.row.labels,
    async (newValue, oldValue) => {
      getLabels(newValue)
    }
);

watch(
    () => props.row.execPath,
    async (newValue, oldValue) => {
      config.execPath = newValue;
    }
);

function getLabels(obj) {
  if (obj) {
    labels.value = Object.keys(obj).map(key => {
      return {
        key: key,
        value: obj[key]
      }
    });
  } else {
    labels.value = []
  }
}

watch(
    () => props.row.launchArguments,
    async (newValue, oldValue) => {
      editor.setValue((newValue || []).join('\n'));
    }
);

// 抽屉复用组件实例：切换到其它实例/行数据刷新（保存后广播）时重新初始化分配设置表单
watch(
    () => props.row,
    (newRow) => {
      alloc.instanceType = newRow.instanceType || 0;
      alloc.whitelistRows = (newRow.whitelist || []).map(ip => ({value: ip, error: ''}));
      alloc.maxPlayerCount = (newRow.maxPlayerCount === undefined || newRow.maxPlayerCount === null) ? -1 : newRow.maxPlayerCount;
    }
);

// 精确 IP（IPv4/IPv6）或 IPv4 通配网段（如 192.168.1.*），与后端 util.NormalizeIPRule 校验规则一致
function isValidIpRule(ip) {
  const text = (ip || '').trim();
  if (!text) {
    return false;
  }
  if (!text.includes('*')) {
    const ipv4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
    const ipv6 = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/;
    if (ipv4.test(text)) {
      return text.split('.').every(n => Number(n) >= 0 && Number(n) <= 255);
    }
    return ipv6.test(text);
  }
  // 通配网段：必须完整 4 段，每段为 * 或 0-255
  const parts = text.split('.');
  if (parts.length !== 4) {
    return false;
  }
  return parts.every(part => part === '*' || (/^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255));
}

function addIpRow() {
  alloc.whitelistRows.push({value: '', error: ''});
}

function clearWhitelist() {
  alloc.whitelistRows = [];
  Notify.create({type: 'info', position: 'top', message: '白名单已清空，点击"保存分配设置"后生效'});
}

// 收集并校验白名单行；返回 null 表示校验失败（错误信息已写到行内）
function collectWhitelist() {
  const values = [];
  for (let i = 0; i < alloc.whitelistRows.length; i++) {
    const row = alloc.whitelistRows[i];
    const text = (row.value || '').trim();
    row.error = '';
    if (!text) {
      continue; // 空白行：忽略（新建未填写的行/误按加号）
    }
    if (!isValidIpRule(text)) {
      row.error = '非法IP或网段';
      continue;
    }
    if (values.includes(text)) {
      row.error = '与上面的行重复';
      continue;
    }
    values.push(text);
  }
  const errors = [];
  alloc.whitelistRows.forEach((row, index) => {
    if (row.error) {
      errors.push(`第${index + 1}行：${row.error}`);
    }
  });
  if (errors.length > 0) {
    Notify.create({
      type: 'negative',
      position: 'top',
      message: `白名单校验失败（${errors.join('；')}），请修正后再保存`
    });
    return null;
  }
  return values;
}

function saveSettings() {
  if (alloc.instanceType === 0 && (alloc.maxPlayerCount < -1 || alloc.maxPlayerCount === 0)) {
    Notify.create({type: 'negative', position: 'top', message: '连接数上限仅支持-1(不限)或>=1'});
    return;
  }
  const whitelist = collectWhitelist();
  if (whitelist === null) {
    return;
  }
  updateInstanceSettings({
    sid: props.row.sid,
    instanceType: alloc.instanceType,
    whitelist,
    maxPlayerCount: alloc.instanceType === 0 ? alloc.maxPlayerCount : -1
  }).then((r) => {
    if (r.code !== 200 || !r.data) {
      return;
    }
    const instance = r.data.instance;
    // 保存成功：用服务端返回的权威值回填表单，并同步到行数据（后端会做通配网段规范化/去重，前端与后端保持一致）
    alloc.instanceType = instance.instanceType;
    alloc.whitelistRows = (instance.whitelist || []).map(ip => ({value: ip, error: ''}));
    alloc.maxPlayerCount = instance.maxPlayerCount;
    props.row.instanceType = instance.instanceType;
    props.row.whitelist = instance.whitelist;
    props.row.maxPlayerCount = instance.maxPlayerCount;
    if (alloc.instanceType === 0
        && r.data.instance.maxPlayerCount > 0
        && r.data.currentPlayerCount > r.data.instance.maxPlayerCount) {
      Notify.create({
        type: 'warning',
        position: 'top',
        message: `当前连接数 ${r.data.currentPlayerCount} 已超新上限 ${r.data.instance.maxPlayerCount}，新连接将被拒绝`
      });
    }
  }).catch(() => {
    Notify.create({type: 'negative', position: 'top', message: '保存失败，请检查服务端状态后重试'});
  });
}

</script>
<template>
  <div class="q-pa-md q-gutter-md">
    <div class="text-h5">实例配置</div>
    <div class="text-subtitle2">实例名称</div>
    <q-input dense filled v-model="config.name" readonly/>
    <div class="text-subtitle2">可执行文件路径</div>
    <q-input dense filled v-model="config.execPath" readonly/>
    <div class="text-subtitle2">启动参数</div>
    <div class="editor" ref="dom"></div>
    <div class="text-subtitle2">自定义标签</div>
    <div class="q-pa-none">
      <q-chip v-for="label in labels">{{ label.key }}: {{ label.value }}</q-chip>
    </div>
    <q-separator class="q-my-md"/>
    <div class="text-h5">分配设置</div>
    <div class="text-subtitle2">实例类型</div>
    <q-option-group
        v-model="alloc.instanceType"
        :options="[
          {label: '共享实例', value: 0},
          {label: '独占实例', value: 1}
        ]"
        inline
    />
    <div class="text-subtitle2">白名单（不配置=所有 IP 可分配；支持精确 IP 与通配网段，如 192.168.1.*）</div>
    <div class="q-gutter-y-sm">
      <div v-for="(row, index) in alloc.whitelistRows" :key="index" class="row items-center q-gutter-sm">
        <q-input dense filled class="col" v-model="row.value"
                 :error="!!row.error" :error-message="row.error"
                 placeholder="192.168.1.* 或 10.0.0.5"
                 @keyup.enter="index === alloc.whitelistRows.length - 1 ? addIpRow() : null">
        </q-input>
        <q-btn dense flat round color="negative" icon="remove_circle_outline"
               :disable="alloc.whitelistRows.length === 1 && !row.value"
               @click="alloc.whitelistRows.splice(index, 1)"/>
      </div>
    </div>
    <div class="q-mt-sm row q-gutter-md">
      <q-btn size="sm" color="primary" icon="add" label="新增IP" @click="addIpRow"/>
      <q-btn size="sm" color="negative" outline label="一键清空白名单"
             :disable="alloc.whitelistRows.length === 0" @click="clearWhitelist"/>
    </div>
    <div class="text-subtitle2">连接数上限</div>
    <q-input v-if="alloc.instanceType === 0" dense filled type="number" v-model.number="alloc.maxPlayerCount"
             hint="-1 表示不限；白名单外的 IP 将无法分配"/>
    <q-input v-else dense filled readonly model-value="固定 1"/>
    <div class="q-mt-md row q-gutter-md">
      <q-btn color="primary" label="保存分配设置" @click="saveSettings"/>
      <q-btn color="white" text-color="primary" label="关闭" @click="$emit('close')"/>
    </div>
  </div>
</template>
<style scoped>
.editor {
  height: 240px;
}
</style>
