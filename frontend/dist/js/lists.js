// ---------------- свои списки ----------------

const LISTS = {
  "zapret-include": { title: "Домены для обхода", hint: "По одному домену в строке. Поддомены включаются сами (yandex.ru = *.yandex.ru); «^» в начале — только сам домен. Шаблоны и regexp тут не работают — их понимает только VPN. Строки с # — комментарии." },
  "zapret-exclude": { title: "Исключения: домены", hint: "Сайты, которые обход не трогает. По одному домену в строке." },
  "zapret-ipexclude": { title: "Исключения: адреса", hint: "IP-адреса и подсети, которые обход не трогает: 1.2.3.4 или 1.2.3.0/24, по одному в строке." },
  "vpn-sites": { title: "Всегда через VPN", hint: "По одному в строке: домен (example.com — с поддоменами; можно вставить ссылку), шаблон (yandex.*, img*.site.ru), regexp:… или keyword:слово, IP или подсеть (1.2.3.0/24), программа (Discord.exe — имя как в Диспетчере задач). Эти правила важнее всех остальных." },
  "vpn-direct": { title: "Всегда напрямую", hint: "По одному в строке: домен, шаблон (yandex.*), regexp:…, keyword:…, IP или подсеть, программа (.exe). Всё это пойдёт мимо VPN, даже если сайт в базе блокировок. Если что-то есть в обоих списках — побеждает «напрямую»." },
};
let listFrom = "viewMain";
let listID = "";

async function openList(id) {
  const meta = LISTS[id];
  if (!meta) return;
  listID = id;
  listFrom = [...document.querySelectorAll(".view")].find((v) => !v.hidden)?.id || "viewMain";
  $("listTitle").textContent = t(meta.title);
  $("listHint").textContent = t(meta.hint);
  $("listStatus").textContent = "";
  $("listStatus").className = "list-status";
  $("listText").value = await api().GetUserList(id);
  show("viewList");
  $("listText").focus();
}

for (const b of document.querySelectorAll("[data-list]")) b.addEventListener("click", () => openList(b.dataset.list));

$("listSave").addEventListener("click", () => guarded(async () => {
  const st = $("listStatus");
  $("listSave").disabled = true;
  const r = await api().SaveUserList(listID, $("listText").value);
  $("listSave").disabled = false;
  if (r.error) {
    st.textContent = r.error;
    st.className = "list-status err";
    return;
  }
  st.textContent = t("Сохранено · записей: {x}", r.count);
  st.className = "list-status ok";
  $("listText").value = await api().GetUserList(listID); // как записалось после проверки
}));
