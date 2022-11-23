import { Box, Flex, Heading, Paragraph } from "theme-ui";
import { useEffect, useState } from "react";

import { useRouter } from "next/router";
import { Layout, QRCode } from "../components";
import axios from "axios";

const Page = (props: {issuerPublicUrl: string, issuerLocalUrl: string}) => {
  const [qrData, setQRData] = useState({});

  const router = useRouter();
  const claimID = router.query.claimID;
  const userID = router.query.userID;
  const dob = `${router.query.dob}`;
  const year = dob.substring(0, 4);
  const month = dob.substring(4, 6);
  const day = dob.substring(6, 8);

  useEffect(() => {

    (async () => {
      await axios.get("http://" + props.issuerLocalUrl + `/api/v1/claims/offers/${userID}/${claimID}`).then((res) => {

        setQRData(res.data);
      })

    })();
  }, [])

    return (
    <Layout>
      <Flex
        sx={{
          flex: 1,
          flexDirection: "column",
          justifyContent: "center",
          alignItems: "center",
        }}
      >
        <Heading sx={{ textAlign: "center", fontSize: [32], my: [4] }}>
          Get Your Claim 🚀
        </Heading>
        <QRCode
          level="Q"
          style={{ width: "100%" }}
          value={JSON.stringify(qrData)}
        />
        <Box>
          <Paragraph sx={{ variant: "text.para" }}>
            This claim proves you were born on {month}/{day}/{year} <span style={{ fontSize: "14px" }}>(mm/dd/yyyy)</span>
          </Paragraph>
        </Box>
      </Flex>
    </Layout>
  );
};


export async function getServerSideProps(context) {
  const yaml = require('js-yaml');
  const fs = require('fs');

  let  issuerPublicUrl = "";
  let  issuerLocalUrl = "";


  try {
    const doc = yaml.load(fs.readFileSync('./../../../issuer/issuer_config.default.yaml', 'utf8'));
    issuerPublicUrl = doc.public_url;
    issuerLocalUrl = doc.local_url;
  } catch (e) {
    console.log("encounter error on load config file, err: " + e);
    process.exit(1);
  }

  return {
    props: {issuerPublicUrl: issuerPublicUrl, issuerLocalUrl: issuerLocalUrl },
  }
}

export default Page;
