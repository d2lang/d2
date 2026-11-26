package e2etests

import (
	_ "embed"
	"testing"
)

func unicodeCases(t *testing.T) []testCase {
	return []testCase{
		{
			name: "japanese-basic",
			script: `a: ああああああああああ
`,
		},
		{
			name: "japanese-full",
			script: `a: "ある日、トマトが道を歩いていたら、道路の向こうからキュウリがやって来ました。\nトマトは驚いて尋ねました。\n「キュウリさん、どうしてあなたはここにいるのですか？」 キュウリは答えました。「あなたと同じ理由でここにいます。サラダになるために。」"

b: "「バナナは皮を剥いて食べるものです。」" {
  style.font-size: 55
}

a -> b: 「バカは死ななきゃ治らない。」
`,
		},
		{
			name: "emojis",
			script: `a: 🙈🙈🙈🙈🙈🙈🙈🙈
✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊✊ -> ☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️☁️
`,
		},
		{
			name: "with-style",
			script: `おやすみなさい: {style.stroke-width: 15; style.double-border: true}
`,
		},
		{
			name: "japanese-mixed",
			script: `a: "トマトが赤くなったのはなぜですか？Because it saw the salad dressing!👩‍👩‍👧‍👶👩‍👩‍👧‍👶"
b: "トマトが赤くなったのはなぜですか？Because it saw the salad dressing!👩‍👩‍👧‍👶👩‍👩‍👧‍👶" {
  style.font-size: 100
}
c: 今日はTokyoでsushiを食べました
d: 先日、Shibuyaで友達とshoppingを楽😊しんだ後、ramen屋でdelicious😊なラーメンを食べた。{
  style.font-size: 43
}
e: English English English
f: 先日先日先日
a -> b -> c -> d -> e -> f

`,
		},
		{
			name: "chinese",
			script: `poem: |md
  床前明月光，

	疑是地上霜。

	举头望明月，

	低头思故乡。
|
a: 所以，即使夏天很热
poem -> a
`,
		},
		{
			name: "korean",
			script: `a: 고생끝에낙이온다
`,
		},
		{
			name: "mixed-language",
			script: `a: 有一个叫做夏天的季节。\n ある季節、夏という名前がついています。\n한 계절, 여름이란 이름이 있습니다.
b: 夏天的时候，天气非常热，人们总是流着汗。
c: |md
  夏になると、とても暑くて、人々は汗を流しています。

  여름에는 매우 더워서 사람들은 땀을 흘립니다.
|
a -> b
a -> c
`,
		},
		{
			name: "mixed-language-2",
			script: `a: 我 (wǒ) - Mandarin Chinese
b: ສະບາຍດີ (sabaai dii) - Lao
c: ជំរាបសួរ (jomreab suor) - Khmer

d: สวัสดี (sà-wàt-dii) - Thai

e: ສະບາຍດີ (sabaidee) - Lao

f: ဟယ်လို (helaou) - Burmese

g: mari (まり) - Ainu

h: cào (草) - Zhuang

i: күнтізбе (kúntízbe) - Kazakh

j: բարև (barev) - Armenian

k: монгол (mongol) - Mongolian

l: mila (میلا) - Uyghur

m: નમસ્તે (namaste) - Gujarati

n: 漢字 (kanji) - Japanese

o: 위 (wi) - Korean

p: 吾哥 (ngǔgāi) - Cantonese

a -> b -> c -> d
e -> f -> g -> h
i -> j -> k -> l
m -> n -> o -> p

မင်္ဂလာပါ (mingalaba) - Burmese

сайн уу (sain uu) - Mongolian

ਸਤਿ ਸ੍ਰੀ ਅਕਾਲ (sat sri akal) - Punjabi

你吃了吗 (ní chī le ma) - Mandarin Chinese

饭 (fan) - Zhuang

مەن سىزنى ياخشى ئۈمىد ق
`,
		},
	}
}
