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
  whitelist: props.row.whitelist ? [...props.row.whitelist] : [],
  maxPlayerCount: (props.row.maxPlayerCount === undefined || props.row.maxPlayerCount === null) ? -1 : props.row.maxPlayerCount
});
const ipInput = ref('');
const dom = ref();
const labels = ref([])
let editor;
onMounted(() => {
  getLabels(props.row.labels)
  editor = monaco.editor.create(dom.value, {
    value: props.row.launchArguments.join('\n'),
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
      editor.setValue(newValue.join('\n'));
    }
);

// 抽屉复用组件实例：切换到其它实例时重新初始化分配设置表单
watch(
    () => props.row,
    (newRow) => {
      alloc.instanceType = newRow.instanceType || 0;
      alloc.whitelist = newRow.whitelist ? [...newRow.whitelist] : [];
      alloc.maxPlayerCount = (newRow.maxPlayerCount === undefined || newRow.maxPlayerCount === null) ? -1 : newRow.maxPlayerCount;
      ipInput.value = '';
    }
);

function isValidIp(ip) {
  const ipv4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
  const ipv6 = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/;
  if (ipv4.test(ip)) {
    return ip.split('.').every(n => Number(n) >= 0 && Number(n) <= 255);
  }
  return ipv6.test(ip);
}

function addIp() {
  const text = (ipInput.value || '').trim();
  if (!text) {
    return;
  }
  // 回车/逗号分隔生成
  for (const part of text.split(/[,，\s]+/).filter(Boolean)) {
    if (!isValidIp(part)) {
      Notify.create({type: 'negative', position: 'top', message: `非法IP: ${part}`});
      continue;
    }
    if (!alloc.whitelist.includes(part)) {
      alloc.whitelist.push(part);
    }
  }
  ipInput.value = '';
}

function saveSettings() {
  if (alloc.instanceType === 0 && (alloc.maxPlayerCount < -1 || alloc.maxPlayerCount === 0)) {
    Notify.create({type: 'negative', position: 'top', message: '连接数上限仅支持-1(不限)或>=1'});
    return;
  }
  updateInstanceSettings({
    sid: props.row.sid,
    instanceType: alloc.instanceType,
    whitelist: alloc.whitelist,
    maxPlayerCount: alloc.instanceType === 1 ? alloc.maxPlayerCount : alloc.maxPlayerCount
  }).then((r) => {
    if (r.code === 200 && r.data && alloc.instanceType === 0
        && r.data.instance.maxPlayerCount > 0
        && r.data.currentPlayerCount > r.data.instance.maxPlayerCount) {
      Notify.create({
        type: 'warning',
        position: 'top',
        message: `当前连接数 ${r.data.currentPlayerCount} 已超新上限 ${r.data.instance.maxPlayerCount}，新连接将被拒绝`
      });
    }
  });
}

</script>
<template>
  <div class="q-pa-md q-gutter-md">
    <div class="text-h5">实例配置</div>
    <div class="text-subtitle2">实例名称</div>
    <q-input dense filled v-model="config.name"/>
    <div class="text-subtitle2">可执行文件路径</div>
    <q-input dense filled v-model="config.execPath"/>
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
    <div class="text-subtitle2">白名单（不配置=所有 IP 可分配）</div>
    <q-input dense filled v-model="ipInput" @keyup.enter="addIp"
             placeholder="输入 IP 后回车添加，多个可用逗号分隔"/>
    <div class="q-pa-none q-mt-sm">
      <q-chip v-for="(ip, index) in alloc.whitelist" :key="ip" removable
              @remove="alloc.whitelist.splice(index, 1)">
        {{ ip }}
      </q-chip>
    </div>
    <div class="text-subtitle2">连接数上限</div>
    <q-input v-if="alloc.instanceType === 0" dense filled type="number" v-model.number="alloc.maxPlayerCount"
             hint="-1 表示不限"/>
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
